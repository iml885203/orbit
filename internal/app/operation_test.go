package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/autoupdate"
	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/instance"
)

func executeOperationRoot(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	oldArgs, oldConfig, oldGiven, oldKind, oldInstance := os.Args, configFile, configFlagGiven, configContextKind, instanceName
	oldJSON, oldStart := cli.JSONOutput, cli.StartCommand
	oldID, oldAt, oldCommand := cliHistID, cliHistAt, cliHistCmd
	defer func() {
		os.Args, configFile, configFlagGiven, configContextKind, instanceName = oldArgs, oldConfig, oldGiven, oldKind, oldInstance
		cli.JSONOutput, cli.StartCommand = oldJSON, oldStart
		cliHistID, cliHistAt, cliHistCmd = oldID, oldAt, oldCommand
	}()
	os.Args = append([]string{"orbit"}, args...)
	restore := captureLifecycleProcessStreams(t)
	defer restore()
	root := newRootCommand()
	root.SetArgs(args)
	err := root.ExecuteContext(t.Context())
	if err != nil {
		err = contextualizeDaemonUnavailable(err)
		printExecutionError(os.Stderr, err)
	}
	out, _ := restore()
	return out, err
}

func TestOperationQueryPreservesRecordedOutcomeWithoutRequiringCurrentConfig(t *testing.T) {
	for _, state := range []string{"accepted", "running", "succeeded", "failed"} {
		t.Run(state, func(t *testing.T) {
			prepareLifecycleProgressTest(t)
			t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
			t.Setenv(instance.EnvName, "")
			var lookups, mutations atomic.Int32
			op := daemon.LifecycleOperation{ID: "record-one", Kind: "down", State: state, Phase: "cleanup", Epoch: 7,
				Generation: 3, ConfigPath: "/previous/project/orbit.yaml", Resources: []string{"api"}, AcceptedAt: time.Unix(100, 0).UTC(), Cleanup: "unavailable", Warnings: []string{"cleanup not verified"}}
			if state == "failed" {
				op.Error, op.Code = "controlled cleanup failure", "lifecycle_failed"
			}
			serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/api/operations/record-one" {
					lookups.Add(1)
					daemon.WriteJSON(w, http.StatusOK, op)
					return
				}
				if r.Method == http.MethodPost && r.URL.Path != "/api/history/cli-event" {
					mutations.Add(1)
				}
				http.NotFound(w, r)
			})
			out, err := executeOperationRoot(t, "--config", filepath.Join(daemon.OrbitDir(), "deleted.yaml"), "--json", "operation", op.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertOneLifecycleEnvelope(t, out, true)
			var envelope struct{ Data daemon.LifecycleOperation }
			if err := json.Unmarshal(out, &envelope); err != nil {
				t.Fatal(err)
			}
			got := envelope.Data
			if got.ID != op.ID || got.State != state || got.ConfigPath != op.ConfigPath || got.Epoch != 7 || got.Generation != 3 || got.Cleanup != "unavailable" || len(got.Warnings) != 1 || got.Error != op.Error || got.Code != op.Code {
				t.Fatalf("lost original operation evidence: %s", out)
			}
			if lookups.Load() != 1 || mutations.Load() != 0 {
				t.Fatalf("lookups=%d mutations=%d", lookups.Load(), mutations.Load())
			}
		})
	}
}

func TestOperationMissingHistoryPreservesCodeAndReadOnlyRecovery(t *testing.T) {
	prepareLifecycleProgressTest(t)
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		daemon.WriteJSON(w, http.StatusNotFound, daemon.APIResponse{Code: "operation_not_found", Error: "history evicted"})
	})
	out, err := executeOperationRoot(t, "--json", "operation", "missing")
	if err == nil {
		t.Fatal("missing operation query succeeded")
	}
	assertOneLifecycleEnvelope(t, out, false)
	var envelope struct {
		Error              cli.JSONError
		RecommendedActions []cli.JSONAction `json:"recommended_actions"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "operation_not_found" || len(envelope.RecommendedActions) != 1 || !strings.Contains(envelope.RecommendedActions[0].Command, "status") || strings.Contains(envelope.Error.Hint, "orbit up") {
		t.Fatalf("unsafe or unclassified query failure: %s", out)
	}
}

func TestOperationQueryDoesNotCreateMissingInstanceOrStartDaemon(t *testing.T) {
	prepareLifecycleProgressTest(t)
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	base := daemon.OrbitDir()
	t.Setenv(instance.EnvBaseHome, base)
	t.Setenv(instance.EnvName, "")
	t.Setenv("ORBIT_NAMESPACE", "")
	runtime, err := instance.Resolve(base, "absent293")
	if err != nil {
		t.Fatal(err)
	}
	out, queryErr := executeOperationRoot(t, "--instance", runtime.Name, "--json", "operation", "missing")
	if queryErr == nil {
		t.Fatal("missing daemon query succeeded")
	}
	assertOneLifecycleEnvelope(t, out, false)
	if _, err := os.Stat(runtime.Home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("query created instance directory: %v", err)
	}
	if strings.Contains(string(out), "orbit up") || !strings.Contains(string(out), "--instance absent293 status") {
		t.Fatalf("query suggests provisioning instead of observation: %s", out)
	}
}

func TestDownTimeoutCanQueryOriginalOperationLaterWithoutRepeatingDown(t *testing.T) {
	prepareLifecycleProgressTest(t)
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	t.Setenv(instance.EnvName, "")
	previousTimeout := timeout
	t.Cleanup(func() { timeout = previousTimeout })
	var submitted atomic.Int32
	var complete atomic.Bool
	beforePath := configFile
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/status":
			daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{ConfigPath: beforePath, Resources: []daemon.ResourceStatus{{Name: "api", State: "healthy"}}})
		case "/api/down":
			submitted.Add(1)
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, OperationID: "later293", AffectedResources: []string{"api"}})
		case "/api/operations/later293":
			state := "running"
			if complete.Load() {
				state = "succeeded"
			}
			daemon.WriteJSON(w, http.StatusOK, daemon.LifecycleOperation{ID: "later293", Kind: "down", State: state, ConfigPath: beforePath, Resources: []string{"api"}, Cleanup: "succeeded"})
		default:
			http.NotFound(w, r)
		}
	})
	restore := captureLifecycleProcessStreams(t)
	cmd := downCmd()
	timeout = 150 * time.Millisecond
	cmd.SetContext(context.Background())
	err := runDown(cmd, nil)
	out, _ := restore()
	if !errors.Is(err, cli.ErrTimeout) {
		t.Fatalf("down error=%v, output=%s", err, out)
	}
	assertOneLifecycleEnvelope(t, out, false)
	if !strings.Contains(string(out), "orbit operation later293 --json") {
		t.Fatalf("missing exact observation recovery: %s", out)
	}
	complete.Store(true)
	result, err := executeOperationRoot(t, "--config", filepath.Join(daemon.OrbitDir(), "changed.yaml"), "--json", "operation", "later293")
	if err != nil {
		t.Fatal(err)
	}
	assertOneLifecycleEnvelope(t, result, true)
	if !strings.Contains(string(result), `"state": "succeeded"`) || submitted.Load() != 1 {
		t.Fatalf("did not observe original operation: submitted=%d, %s", submitted.Load(), result)
	}
}

func TestOperationCommandPolicyIsObservation(t *testing.T) {
	root := newRootCommand()
	cmd, _, err := root.Find([]string{"operation"})
	if err != nil || cmd == root {
		t.Fatalf("operation is not discoverable: %v", err)
	}
	if cmd.Hidden || updateMutationCommand(cmd) || commandRequiresAvailableEnvironment(cmd) || commandRequiresMatchingDaemonConfig(cmd) || commandRequiresReconciledDaemon(cmd) {
		t.Fatal("operation was classified as a lifecycle mutation")
	}
}

func TestOperationNamedInstanceObservesPendingUpdateWithoutRecoveringItsJournal(t *testing.T) {
	prepareLifecycleProgressTest(t)
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	base := daemon.OrbitDir()
	t.Setenv(instance.EnvBaseHome, base)
	t.Setenv(instance.EnvName, "")
	t.Setenv("ORBIT_NAMESPACE", "")
	t.Setenv("ORBIT_HOME", base)
	runtime, err := instance.Activate("ci293")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtime.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	launch := filepath.Join(base, "owned-orbit")
	t.Setenv(autoupdate.EnvLaunchPath, launch)
	if _, err := autoupdate.BeginTransaction(launch, "update", "v1.4.1"); err != nil {
		t.Fatal(err)
	}
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		daemon.WriteJSON(w, http.StatusOK, daemon.LifecycleOperation{ID: "named-record", Kind: "down", State: "running", Instance: runtime.Name, ConfigPath: "/original/orbit.yaml"})
	})
	selected := filepath.Join(base, "deleted config.yaml")
	out, err := executeOperationRoot(t, "--instance", runtime.Name, "--config", selected, "--json", "operation", "named-record")
	if err != nil {
		t.Fatal(err)
	}
	assertOneLifecycleEnvelope(t, out, true)
	var envelope struct {
		Instance           string
		Data               daemon.LifecycleOperation
		RecommendedActions []cli.JSONAction `json:"recommended_actions"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Instance != runtime.Name || envelope.Data.Instance != runtime.Name || len(envelope.RecommendedActions) != 1 {
		t.Fatalf("lost instance identity: %s", out)
	}
	next := envelope.RecommendedActions[0].Command
	if strings.Count(next, "--instance") != 1 || !strings.Contains(next, "--config") || !strings.Contains(next, selected) || !strings.Contains(next, "operation named-record --json") {
		t.Fatalf("lost targeted recovery: %s", out)
	}
	state, err := autoupdate.Load(launch)
	if err != nil || state.Transaction == nil || state.Transaction.FinishedAt != nil {
		t.Fatalf("query rewrote update transaction: %+v, %v", state, err)
	}
}

func TestOperationHumanOutputSeparatesFailedOperationFromSuccessfulQuery(t *testing.T) {
	prepareLifecycleProgressTest(t)
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		daemon.WriteJSON(w, http.StatusOK, daemon.LifecycleOperation{ID: "failed-human", Kind: "down", State: "failed", Cleanup: "unavailable", Code: "cleanup_failed", Error: "controlled failure", Warnings: []string{"not verified"}})
	})
	out, err := executeOperationRoot(t, "operation", "failed-human")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"State: failed", "Cleanup: unavailable", "controlled failure", "not verified", "Query succeeded"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("missing %q from human evidence: %s", want, out)
		}
	}
}

func TestOperationRejectsMismatchedOrUnknownRecord(t *testing.T) {
	for _, op := range []daemon.LifecycleOperation{{ID: "other", State: "succeeded"}, {ID: "requested", State: "unknown"}} {
		t.Run(op.ID+op.State, func(t *testing.T) {
			prepareLifecycleProgressTest(t)
			t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
			serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) { daemon.WriteJSON(w, http.StatusOK, op) })
			out, err := executeOperationRoot(t, "--json", "operation", "requested")
			if err == nil {
				t.Fatal("untrustworthy operation was reported as a successful query")
			}
			assertOneLifecycleEnvelope(t, out, false)
		})
	}
}

func TestOperationIgnoresInvalidCurrentConfigAndSourceRegistry(t *testing.T) {
	prepareLifecycleProgressTest(t)
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	for _, path := range []string{configFile, filepath.Join(daemon.OrbitDir(), "sources.json")} {
		if err := os.WriteFile(path, []byte("{ invalid"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		daemon.WriteJSON(w, http.StatusOK, daemon.LifecycleOperation{ID: "retained", Kind: "down", State: "succeeded", ConfigPath: "/original/orbit.yaml"})
	})
	out, err := executeOperationRoot(t, "--config", configFile, "--json", "operation", "retained")
	if err != nil {
		t.Fatal(err)
	}
	assertOneLifecycleEnvelope(t, out, true)
	if !strings.Contains(string(out), "/original/orbit.yaml") {
		t.Fatalf("lost original context: %s", out)
	}
}
