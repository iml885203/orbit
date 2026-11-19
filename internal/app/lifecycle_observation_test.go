package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
)

func TestHumanWaitDeadlineInterruptsBlockedStatus(t *testing.T) {
	prepareLifecycleProgressTest(t)
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(450 * time.Millisecond):
		}
		_ = json.NewEncoder(w).Encode(daemon.StatusResponse{})
	})
	start := time.Now()
	err := pollLoop(daemon.NewClient(daemon.DefaultSocketPath()), 150*time.Millisecond, "resources to stop", func(time.Time, *daemon.StatusResponse) (bool, error) { return false, nil })
	if err == nil {
		t.Fatal("blocked status wait succeeded")
	}
	if elapsed := time.Since(start); elapsed > 350*time.Millisecond {
		t.Fatalf("blocked status exceeded CLI deadline: %s", elapsed)
	}
}

func TestNonTerminalStopWaitReportsStoppingBeforeCompletion(t *testing.T) {
	prepareLifecycleProgressTest(t)
	oldTimeout := timeout
	timeout = 3 * time.Second
	t.Cleanup(func() { timeout = oldTimeout })
	t.Setenv("NO_COLOR", "1")
	started := time.Now()
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		state := "stopping"
		if time.Since(started) > 300*time.Millisecond {
			state = "stopped"
		}
		daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{Resources: []daemon.ResourceStatus{{Name: "api", State: state}}})
	})
	restore := captureLifecycleProcessStreams(t)
	defer restore()
	err := waitForServicesStopped(daemon.NewClient(daemon.DefaultSocketPath()), []string{"api"}, false)
	out, _ := restore()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "api stopping") {
		t.Fatalf("no non-terminal progress: %q", out)
	}
}

func TestDownExposesWaitTimeout(t *testing.T) {
	flag := downCmd().Flags().Lookup("timeout")
	if flag == nil || flag.DefValue != "0s" {
		t.Fatalf("down timeout flag=%v", flag)
	}
}

func TestDownJSONObservesExplicitFailureWithoutStoppingSnapshot(t *testing.T) {
	testDownExplicitFailure(t, "healthy")
}

func TestDownJSONChecksSweepWhenTrackedResourcesAlreadyStopped(t *testing.T) {
	testDownExplicitFailure(t, "stopped")
}

func testDownExplicitFailure(t *testing.T, state string) {
	t.Helper()
	prepareLifecycleProgressTest(t)
	var submitted atomic.Int32
	var pinned atomic.Bool
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/status":
			daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{Epoch: 7, Generation: 3, ConfigPath: configFile, Resources: []daemon.ResourceStatus{{Name: "api", State: state}}})
		case "/api/down":
			submitted.Add(1)
			pinned.Store(r.Header.Get(daemon.ExpectedEpochHeader) == "7" && r.Header.Get(daemon.ExpectedGenerationHeader) == "3")
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, Message: "stopping", OperationID: "stop-one", AffectedResources: []string{"api"}})
		case "/api/operations/stop-one":
			daemon.WriteJSON(w, http.StatusOK, daemon.LifecycleOperation{ID: "stop-one", Kind: "down", Epoch: 7, Generation: 3, State: "failed", Phase: "completed", ConfigPath: configFile, Resources: []string{"api"}, Error: "controlled stop failure", Code: "lifecycle_failed"})
		default:
			http.NotFound(w, r)
		}
	})
	restore := captureLifecycleProcessStreams(t)
	defer restore()
	start := time.Now()
	cmd := downCmd()
	cmd.SetContext(t.Context())
	err := runDown(cmd, nil)
	out, diagnostics := restore()
	if err == nil || !strings.Contains(err.Error(), "controlled stop failure") {
		t.Fatalf("error=%v", err)
	}
	if time.Since(start) > 700*time.Millisecond {
		t.Fatal("explicit stop failure waited for resource sampling")
	}
	assertOneLifecycleEnvelope(t, out, false)
	var envelope struct {
		Data struct {
			ID        string                  `json:"operation_id"`
			State     string                  `json:"operation_state"`
			Resources []daemon.ResourceStatus `json:"resources"`
		}
	}
	if json.Unmarshal(out, &envelope) != nil || envelope.Data.ID != "stop-one" || envelope.Data.State != "failed" {
		t.Fatalf("lost operation result: %s", out)
	}
	if len(envelope.Data.Resources) != 0 {
		t.Fatalf("pre-acceptance snapshot reported as final: %s", out)
	}
	if !strings.Contains(diagnostics, "accepted:") {
		t.Fatalf("no acceptance progress: %q", diagnostics)
	}
	if submitted.Load() != 1 || !pinned.Load() {
		t.Fatal("repeated stop mutation")
	}
}

func TestDownJSONCancelDetachesAcceptedOperationWithoutRepeatingMutation(t *testing.T) {
	prepareLifecycleProgressTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var submitted atomic.Int32
	var observationCancelled atomic.Bool
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/status":
			daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{ConfigPath: configFile, Resources: []daemon.ResourceStatus{{Name: "api", State: "healthy"}}})
		case "/api/down":
			submitted.Add(1)
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, Message: "stopping", OperationID: "stop-detach", AffectedResources: []string{"api"}})
		case "/api/operations/stop-detach":
			cancel()
			<-r.Context().Done()
			observationCancelled.Store(true)
		default:
			http.NotFound(w, r)
		}
	})
	restore := captureLifecycleProcessStreams(t)
	defer restore()
	cmd := downCmd()
	cmd.SetContext(ctx)
	err := runDown(cmd, nil)
	out, diagnostics := restore()
	if !errors.Is(err, cli.ErrCanceled) {
		t.Fatalf("error=%v", err)
	}
	assertOneLifecycleEnvelope(t, out, false)
	if !strings.Contains(string(out), "stop-detach") || !strings.Contains(string(out), "may continue") {
		t.Fatalf("lost detached operation context: %s", out)
	}
	if !strings.Contains(diagnostics, "accepted:") || submitted.Load() != 1 {
		t.Fatalf("progress=%q submitted=%d", diagnostics, submitted.Load())
	}
	deadline := time.Now().Add(time.Second)
	for !observationCancelled.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !observationCancelled.Load() {
		t.Fatal("observation request was not canceled")
	}
}

func TestRestartJSONWaitsForReadinessAfterOperationCompletes(t *testing.T) {
	testRestartOperationReadiness(t, false)
}

func TestRestartJSONRejectsNewGenerationDuringReadiness(t *testing.T) {
	testRestartOperationReadiness(t, true)
}

func testRestartOperationReadiness(t *testing.T, changed bool) {
	t.Helper()
	prepareLifecycleProgressTest(t)
	var restarted atomic.Bool
	var afterOperation atomic.Int32
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/status":
			state := "healthy"
			if restarted.Load() {
				state = "starting"
				if afterOperation.Add(1) > 2 {
					state = "healthy"
				}
			}
			generation := uint64(3)
			if changed && afterOperation.Load() > 1 {
				generation = 4
			}
			daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{Epoch: 7, Generation: generation, ConfigPath: configFile, Resources: []daemon.ResourceStatus{{Name: "api", State: state}}})
		case "/api/restart/api":
			restarted.Store(true)
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, Message: "restarting api", OperationID: "restart-one", AffectedResources: []string{"api"}})
		case "/api/operations/restart-one":
			daemon.WriteJSON(w, http.StatusOK, daemon.LifecycleOperation{ID: "restart-one", Kind: "restart", Epoch: 7, Generation: 3, State: "succeeded", Phase: "completed", ConfigPath: configFile, Resources: []string{"api"}})
		default:
			http.NotFound(w, r)
		}
	})
	restore := captureLifecycleProcessStreams(t)
	defer restore()
	cmd := restartCmd()
	cmd.SetContext(t.Context())
	err := runRestart(cmd, []string{"api"})
	out, diagnostics := restore()
	if changed {
		if err == nil || !strings.Contains(err.Error(), "no longer matches") {
			t.Fatalf("accepted generation change: %v", err)
		}
		assertOneLifecycleEnvelope(t, out, false)
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	assertOneLifecycleEnvelope(t, out, true)
	if afterOperation.Load() < 3 || !strings.Contains(string(out), `"state": "healthy"`) {
		t.Fatalf("finished before readiness: statuses=%d output=%s", afterOperation.Load(), out)
	}
	if !strings.Contains(diagnostics, "waiting for readiness") {
		t.Fatalf("missing readiness progress: %q", diagnostics)
	}
}

func TestAcceptedLifecycleRejectsChangedGeneration(t *testing.T) {
	prepareLifecycleProgressTest(t)
	before := &daemon.StatusResponse{Epoch: 7, Generation: 3, ConfigPath: configFile}
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		daemon.WriteJSON(w, http.StatusOK, daemon.LifecycleOperation{ID: "old-operation", State: "succeeded", Epoch: 7, Generation: 4, ConfigPath: configFile})
	})
	progress := newLifecycleProgress(context.Background(), io.Discard, time.Now())
	defer progress.Close()
	_, err := waitForAcceptedLifecycle(context.Background(), daemon.NewClient(daemon.DefaultSocketPath()), before, &daemon.APIResponse{OperationID: "old-operation"}, nil, progress, time.Now())
	var fault lifecycleObservationError
	if !errors.As(err, &fault) || fault.code != "operation_target_changed" {
		t.Fatalf("accepted changed generation: %v", err)
	}
}

func TestDownJSONDeadlineSurvivesStalledOperationStatus(t *testing.T) {
	prepareLifecycleProgressTest(t)
	var submitted atomic.Int32
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/status":
			daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{ConfigPath: configFile, Resources: []daemon.ResourceStatus{{Name: "api", State: "healthy"}}})
		case "/api/down":
			submitted.Add(1)
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, Message: "stopping", OperationID: "slow-stop", AffectedResources: []string{"api"}})
		case "/api/operations/slow-stop":
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	})
	restore := captureLifecycleProcessStreams(t)
	defer restore()
	cmd := downCmd()
	cmd.SetContext(t.Context())
	oldTimeout := timeout
	timeout = 200 * time.Millisecond
	t.Cleanup(func() { timeout = oldTimeout })
	started := time.Now()
	err := runDown(cmd, nil)
	out, diagnostics := restore()
	if !errors.Is(err, cli.ErrTimeout) {
		t.Fatalf("error=%v", err)
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("stalled operation request blocked observation deadline")
	}
	assertOneLifecycleEnvelope(t, out, false)
	if !strings.Contains(diagnostics, "accepted:") || !strings.Contains(string(out), "slow-stop") || submitted.Load() != 1 {
		t.Fatalf("lost accepted operation: output=%s stderr=%q submissions=%d", out, diagnostics, submitted.Load())
	}
}

func TestHumanDownCancellationDetachesWithoutSuccess(t *testing.T) {
	prepareLifecycleProgressTest(t)
	cli.JSONOutput = false
	t.Setenv("NO_COLOR", "1")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var submitted atomic.Int32
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/status":
			daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{ConfigPath: configFile, Resources: []daemon.ResourceStatus{{Name: "api", State: "healthy"}}})
		case "/api/down":
			submitted.Add(1)
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, Message: "stopping", OperationID: "detach-human", AffectedResources: []string{"api"}})
		case "/api/operations/detach-human":
			cancel()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	})
	restore := captureLifecycleProcessStreams(t)
	defer restore()
	cmd := downCmd()
	cmd.SetContext(ctx)
	err := runDown(cmd, nil)
	out, _ := restore()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "detached") || !strings.Contains(string(out), "may continue") || strings.Contains(string(out), "Environment stopped") {
		t.Fatalf("detachment reported completion: %q", out)
	}
	if submitted.Load() != 1 {
		t.Fatal("detachment repeated accepted mutation")
	}
}

func TestRootLifecycleDeadlineBoundsDaemonPrecheck(t *testing.T) {
	for _, command := range []string{"down", "restart"} {
		for _, blocked := range []string{"/api/health", "/api/status"} {
			t.Run(command+blocked, func(t *testing.T) {
				previousFlag, previousKind, previousInstance, previousTimeout := configFlagGiven, configContextKind, instanceName, timeout
				previousCLIStart := cli.StartCommand
				previousHistoryID, previousHistoryAt, previousHistoryCommand := cliHistID, cliHistAt, cliHistCmd
				t.Cleanup(func() {
					configFlagGiven, configContextKind, instanceName, timeout = previousFlag, previousKind, previousInstance, previousTimeout
					cli.StartCommand = previousCLIStart
					cliHistID, cliHistAt, cliHistCmd = previousHistoryID, previousHistoryAt, previousHistoryCommand
				})
				prepareLifecycleProgressTest(t)
				t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
				if err := os.WriteFile(configFile, []byte("version: \"3\"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				var mutations atomic.Int32
				serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost && (r.URL.Path == "/api/down" || strings.HasPrefix(r.URL.Path, "/api/restart/")) {
						mutations.Add(1)
					}
					if r.URL.Path == blocked {
						select {
						case <-r.Context().Done():
							return
						case <-time.After(600 * time.Millisecond):
						}
					}
					switch r.URL.Path {
					case "/api/health":
						w.WriteHeader(http.StatusOK)
					case "/api/status":
						daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{ConfigPath: configFile, Resources: []daemon.ResourceStatus{{Name: "api", State: "healthy"}}})
					default:
						http.NotFound(w, r)
					}
				})
				restore := captureLifecycleProcessStreams(t)
				defer restore()
				path := configFile
				root := newRootCommand()
				args := []string{"--config", path, "--json", command, "--timeout", "150ms"}
				if command == "restart" {
					args = append(args, "api")
				}
				root.SetArgs(args)
				started := time.Now()
				err := root.ExecuteContext(t.Context())
				if !errors.Is(err, cli.ErrTimeout) {
					t.Fatalf("error=%v", err)
				}
				if elapsed := time.Since(started); elapsed > 450*time.Millisecond {
					t.Fatalf("Cobra precheck exceeded lifecycle budget: %s", elapsed)
				}
				if mutations.Load() != 0 {
					t.Fatalf("precheck sent %d lifecycle mutations after deadline", mutations.Load())
				}
			})
		}
	}
}

func TestLifecycleCleanupWarningsPreserveTerminalResult(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			prepareLifecycleProgressTest(t)
			warning := "Orphan container cleanup could not be verified because Docker is unavailable."
			op := &daemon.LifecycleOperation{State: "succeeded", Cleanup: "unavailable", Warnings: []string{warning}}
			var operationErr error
			message := "Stopping failed; inspect current resource state."
			if failed {
				op.State = "failed"
				operationErr = errors.New("host stop failed")
			}
			restore := captureLifecycleProcessStreams(t)
			defer restore()
			err := finishLifecycleObservation(lifecycleObservationResult{Operation: op}, &daemon.APIResponse{OperationID: "stop-one"}, time.Now(), operationErr, lifecycleJSONOptions{Message: message}, nil)
			out, diagnostics := restore()
			assertOneLifecycleEnvelope(t, out, !failed)
			var envelope struct{ Data lifecycleJSONData }
			if json.Unmarshal(out, &envelope) != nil || envelope.Data.Cleanup != "unavailable" || len(envelope.Data.Warnings) != 1 || !strings.Contains(diagnostics, warning) {
				t.Fatalf("lost cleanup warning: %s / %s", out, diagnostics)
			}
			if failed && (err == nil || envelope.Data.Message != operationErr.Error()) {
				t.Fatalf("failure falsely reported configured resources stopped: %s", out)
			}
			if !failed && (err != nil || !strings.Contains(envelope.Data.Message, "cleanup remains unverified")) {
				t.Fatalf("success overclaimed cleanup: %s", out)
			}
		})
	}
}
