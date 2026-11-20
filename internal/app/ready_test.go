package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iml885203/orbit/internal/autoupdate"
	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/daemonsrv"
	"github.com/iml885203/orbit/internal/instance"
)

func readyFixture(t *testing.T) *daemon.StatusResponse {
	t.Helper()
	prepareLifecycleProgressTest(t)
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	t.Setenv(instance.EnvName, "")
	if err := os.WriteFile(configFile, []byte(`version: "3"
services:
  api:
    command: missing-sdk
    ports:
      http: 18080
    env:
      API_KEY: secret-never-output
  web:
    command: missing-other-sdk
`), 0600); err != nil {
		t.Fatal(err)
	}
	return &daemon.StatusResponse{Epoch: 42, Generation: 7, ConfigPath: configFile, Resources: []daemon.ResourceStatus{
		{Name: "api", State: "healthy", Ports: map[string]int{"http": 32123}, URL: "http://localhost:32123"},
		{Name: "web", State: "stopped"},
	}}
}

func decodeReady(t *testing.T, output []byte, success bool) readyJSONData {
	t.Helper()
	assertOneLifecycleEnvelope(t, output, success)
	var envelope struct{ Data readyJSONData }
	if err := json.Unmarshal(output, &envelope); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "secret-never-output") {
		t.Fatal("environment secret leaked")
	}
	return envelope.Data
}

func TestReadyHealthySubsetIgnoresUnrelatedStoppedAndMissingTools(t *testing.T) {
	status := readyFixture(t)
	var reads, mutations atomic.Int32
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutations.Add(1)
			http.Error(w, "mutation", 500)
			return
		}
		reads.Add(1)
		switch r.URL.Path {
		case "/api/status":
			daemon.WriteJSON(w, 200, status)
		case "/api/version":
			daemon.WriteJSON(w, 200, daemon.VersionResponse{Running: buildVersion()})
		default:
			http.NotFound(w, r)
		}
	})
	out, err := executeOperationRoot(t, "--config", configFile, "ready", "api", "api", "--json")
	if err != nil {
		t.Fatalf("ready: %v\n%s", err, out)
	}
	data := decodeReady(t, out, true)
	if !data.Ready || len(data.RequiredResources) != 1 || len(data.Resources) != 1 || data.Resources[0].Ports["http"] != 32123 || data.Resources[0].URL != "http://localhost:32123" {
		t.Fatalf("data=%+v", data)
	}
	if data.Observation == nil || data.Observation.Epoch != 42 || data.Observation.Generation != 7 || data.Observation.EndpointSource != "daemon_status" || data.Observation.ConfigPath != configFile {
		t.Fatalf("observation=%+v", data.Observation)
	}
	if reads.Load() != 2 || mutations.Load() != 0 {
		t.Fatalf("reads=%d mutations=%d", reads.Load(), mutations.Load())
	}
}

func TestReadyFailsClosedAcrossResourceAndRuntimeBoundaries(t *testing.T) {
	for _, scenario := range []string{"stopped", "starting", "degraded", "pending", "building", "restarting", "stopping", "unknown_state", "missing", "wrong_config", "wrong_instance", "stale_config", "missing_epoch", "duplicate_runtime", "version_mismatch", "missing_version", "status_failure", "invalid_schema", "unknown_name"} {
		t.Run(scenario, func(t *testing.T) {
			status := readyFixture(t)
			wanted := "api"
			switch scenario {
			case "wrong_config":
				status.ConfigPath = filepath.Join(filepath.Dir(configFile), "other.yaml")
			case "wrong_instance":
				status.Instance = "foreign"
			case "stale_config":
				status.ConfigStale = true
				status.ConfigStaleReason = "environment edited"
			case "missing_epoch":
				status.Epoch = 0
			case "missing":
				status.Resources = status.Resources[1:]
			case "duplicate_runtime":
				status.Resources = append(status.Resources, status.Resources[0])
			case "invalid_schema":
				if err := os.WriteFile(configFile, []byte("version: \"999\"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unknown_name":
				wanted = "absent"
			case "version_mismatch", "missing_version", "status_failure":
			default:
				status.Resources[0].State = scenario
			}
			var mutations atomic.Int32
			serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					mutations.Add(1)
					http.Error(w, "mutation", 500)
					return
				}
				switch r.URL.Path {
				case "/api/status":
					if scenario == "status_failure" {
						http.Error(w, "unavailable", 500)
						return
					}
					daemon.WriteJSON(w, 200, status)
				case "/api/version":
					if scenario == "missing_version" {
						http.NotFound(w, r)
						return
					}
					v := buildVersion()
					if scenario == "version_mismatch" {
						v = "foreign"
					}
					daemon.WriteJSON(w, 200, daemon.VersionResponse{Running: v})
				default:
					http.NotFound(w, r)
				}
			})
			out, err := executeOperationRoot(t, "--config", configFile, "ready", wanted, "--json")
			if err == nil {
				t.Fatalf("unexpected pass: %s", out)
			}
			data := decodeReady(t, out, false)
			if data.Ready || len(data.Resources) != 1 || data.Summary == "" {
				t.Fatalf("data=%+v", data)
			}
			if mutations.Load() != 0 {
				t.Fatal("assertion mutated daemon")
			}
			if scenario == "wrong_config" || scenario == "wrong_instance" || scenario == "stale_config" || scenario == "version_mismatch" || scenario == "missing_version" {
				if data.Observation != nil || len(data.Resources[0].Ports) != 0 || data.Resources[0].URL != "" {
					t.Fatalf("untrusted endpoints leaked: %+v", data)
				}
			}
		})
	}
}

func TestReadyMissingDaemonDoesNotCreateNamedInstance(t *testing.T) {
	readyFixture(t)
	base := daemon.OrbitDir()
	t.Setenv(instance.EnvBaseHome, base)
	t.Setenv("ORBIT_NAMESPACE", "")
	target, err := instance.Resolve(base, "absent295")
	if err != nil {
		t.Fatal(err)
	}
	out, err := executeOperationRoot(t, "--instance", target.Name, "--config", configFile, "ready", "api", "--json")
	if err == nil {
		t.Fatal("missing daemon passed")
	}
	decodeReady(t, out, false)
	if _, err := os.Stat(target.Home); !os.IsNotExist(err) {
		t.Fatalf("instance home created: %v", err)
	}
	if !strings.Contains(string(out), "--instance absent295") || !strings.Contains(string(out), "--config") {
		t.Fatalf("lost targeting: %s", out)
	}
}

func TestReadyIsReadOnlyAndRequiresExplicitResources(t *testing.T) {
	root := newRootCommand()
	cmd, _, err := root.Find([]string{"ready"})
	if err != nil {
		t.Fatal(err)
	}
	if updateMutationCommand(cmd) || shouldRecordCLI(cmd) || commandRequiresReconciledDaemon(cmd) || commandRequiresMatchingDaemonConfig(cmd) || lifecycleCommandHasBudget(cmd) {
		t.Fatal("ready entered mutating command policy")
	}
	if !readOnlyEnvironmentCommand(cmd) {
		t.Fatal("ready lost readonly preparation")
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Fatal("empty resource list accepted")
	}
}

func TestReadyHumanReportRetainsAssertionExit(t *testing.T) {
	status := readyFixture(t)
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			daemon.WriteJSON(w, 200, status)
		case "/api/version":
			daemon.WriteJSON(w, 200, daemon.VersionResponse{Running: buildVersion()})
		default:
			http.NotFound(w, r)
		}
	})
	old := cli.JSONOutput
	defer func() { cli.JSONOutput = old }()
	out, err := executeOperationRoot(t, "--config", configFile, "ready", "web")
	if err == nil || !strings.Contains(string(out), "web: stopped") {
		t.Fatalf("err=%v out=%s", err, out)
	}
	if strings.Contains(string(out), "Error:") {
		t.Fatalf("duplicate human report: %s", out)
	}
}

func TestReadyKeepsPendingUpdateTransactionUntouched(t *testing.T) {
	status := readyFixture(t)
	launch := filepath.Join(daemon.OrbitDir(), "owned-orbit")
	t.Setenv(autoupdate.EnvLaunchPath, launch)
	if _, err := autoupdate.BeginTransaction(launch, "update", "v999.0.0"); err != nil {
		t.Fatal(err)
	}
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			daemon.WriteJSON(w, 200, status)
		case "/api/version":
			daemon.WriteJSON(w, 200, daemon.VersionResponse{Running: buildVersion()})
		default:
			http.NotFound(w, r)
		}
	})
	out, err := executeOperationRoot(t, "--config", configFile, "ready", "api", "--json")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	decodeReady(t, out, true)
	state, err := autoupdate.Load(launch)
	if err != nil || state.Transaction == nil || state.Transaction.FinishedAt != nil {
		t.Fatalf("rewrote transaction: %+v %v", state, err)
	}
}

func TestReadyUnavailableSelectionDoesNotTrustAnotherRuntime(t *testing.T) {
	readyFixture(t)
	missing := filepath.Join(daemon.OrbitDir(), "envs", "removed.yaml")
	if err := os.WriteFile(daemonsrv.CurrentEnvPath(), []byte(missing+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := executeOperationRoot(t, "--config", missing, "ready", "api", "--json")
	if err == nil {
		t.Fatal("unavailable selection passed")
	}
	data := decodeReady(t, out, false)
	if data.Observation != nil {
		t.Fatal("trusted unrelated runtime")
	}
	var envelope struct{ Error cli.JSONError }
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "environment_selection_required" {
		t.Fatalf("lost selection error: %s", out)
	}
}

func TestReadyRootDoesNotMigrateLegacySources(t *testing.T) {
	status := readyFixture(t)
	home := daemon.OrbitDir()
	t.Setenv("ORBIT_ENV_REPO_URL", "https://example.invalid/legacy.git")
	t.Setenv("WORKSPACE_ROOT", "")
	envs := envsDestDir()
	if err := os.MkdirAll(envs, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(envs, "legacy.yaml")
	contents, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, contents, 0600); err != nil {
		t.Fatal(err)
	}
	settings := []byte(`{"env_repo_url":"https://example.invalid/legacy.git","workspace_root":"legacy"}`)
	if err := os.WriteFile(daemon.DefaultSettingsPath(), settings, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daemonsrv.CurrentEnvPath(), []byte(legacy+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	status.ConfigPath = legacy
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			daemon.WriteJSON(w, 200, status)
		case "/api/version":
			daemon.WriteJSON(w, 200, daemon.VersionResponse{Running: buildVersion()})
		default:
			http.NotFound(w, r)
		}
	})
	beforeFiles := map[string][]byte{}
	for _, path := range []string{legacy, daemon.DefaultSettingsPath(), daemonsrv.CurrentEnvPath()} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		beforeFiles[path] = content
	}
	beforeDirs := map[string][]string{}
	for _, path := range []string{home, envs} {
		entries, err := os.ReadDir(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			beforeDirs[path] = append(beforeDirs[path], entry.Name())
		}
	}
	out, err := executeOperationRoot(t, "--config", legacy, "ready", "api", "--json")
	if err != nil {
		t.Fatalf("legacy readiness: %v %s", err, out)
	}
	decodeReady(t, out, true)
	for path, before := range beforeFiles {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("changed legacy file %s: %v", path, err)
		}
	}
	for path, before := range beforeDirs {
		entries, err := os.ReadDir(path)
		if err != nil {
			t.Fatal(err)
		}
		after := []string{}
		for _, entry := range entries {
			after = append(after, entry.Name())
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("changed legacy directory %s: %v -> %v", path, before, after)
		}
	}
	if strings.Contains(string(out), "environment_source_migrated") {
		t.Fatalf("unexpected migration notice: %s", out)
	}
}

func TestReadyWithholdsCredentialsFromObservedApplicationURL(t *testing.T) {
	status := readyFixture(t)
	raw := "http://alice:secret-never-output@localhost:18080/path?token=query-secret#fragment-secret"
	contents, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), "command: missing-sdk", "command: missing-sdk\n    url: "+raw, 1))
	if err := os.WriteFile(configFile, contents, 0600); err != nil {
		t.Fatal(err)
	}
	status.Resources[0].URL = raw
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			daemon.WriteJSON(w, 200, status)
		case "/api/version":
			daemon.WriteJSON(w, 200, daemon.VersionResponse{Running: buildVersion()})
		default:
			http.NotFound(w, r)
		}
	})
	out, err := executeOperationRoot(t, "--config", configFile, "ready", "api", "--json")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	data := decodeReady(t, out, true)
	if strings.Contains(string(out), "alice") || strings.Contains(string(out), "query-secret") || strings.Contains(string(out), "fragment-secret") {
		t.Fatalf("credential-bearing URL leaked: %s", out)
	}
	if data.Resources[0].URL != "http://localhost:18080/path" || !data.Resources[0].URLRedacted {
		t.Fatalf("safe endpoint=%q", data.Resources[0].URL)
	}
}

func TestReadyDoesNotCopyCredentialBearingRuntimeReasons(t *testing.T) {
	for _, format := range []string{"json", "human"} {
		t.Run(format, func(t *testing.T) {
			status := readyFixture(t)
			status.Resources[0].State = "degraded"
			status.Resources[0].FailureKind = "health"
			status.Resources[0].StateReason = "HTTP 401 from http://alice:reason-password@localhost:32123/health?token=reason-query#reason-fragment; sensitive-output"
			serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/status":
					daemon.WriteJSON(w, 200, status)
				case "/api/version":
					daemon.WriteJSON(w, 200, daemon.VersionResponse{Running: buildVersion()})
				default:
					http.NotFound(w, r)
				}
			})
			args := []string{"--config", configFile, "ready", "api"}
			if format == "json" {
				args = append(args, "--json")
			}
			out, err := executeOperationRoot(t, args...)
			if err == nil {
				t.Fatal("degraded resource passed")
			}
			if strings.Contains(string(out), "reason-") || strings.Contains(string(out), "sensitive-output") || strings.Contains(string(out), "alice") {
				t.Fatalf("raw runtime reason leaked: %s", out)
			}
			if format == "json" {
				data := decodeReady(t, out, false)
				if data.Resources[0].Reason == "" {
					t.Fatal("missing readiness explanation")
				}
			}
			if !strings.Contains(string(out), "health check") {
				t.Fatalf("missing safe health failure explanation: %s", out)
			}
		})
	}
}
