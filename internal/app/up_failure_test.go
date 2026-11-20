package app

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iml885203/orbit/internal/daemon"
)

func TestRootUpJSONFailureEvidence(t *testing.T) {
	exitCode := 137
	for _, tc := range []struct {
		name          string
		resources     []daemon.ResourceStatus
		affected      []string
		logs          map[string][]string
		missingStatus bool
		want          []map[string]any
	}{
		{name: "image pull preserves registry and platform diagnostics", affected: []string{"api"},
			resources: []daemon.ResourceStatus{{Name: "api", State: "degraded", StateReason: "pulling image registry.example/app:1 (platform linux/arm64): registry unavailable"}},
			want:      []map[string]any{{"name": "api", "kind": "", "state": "degraded", "state_reason": "pulling image registry.example/app:1 (platform linux/arm64): registry unavailable"}}},
		{name: "only unhealthy requested resources carry evidence", affected: []string{"db", "api", "worker"},
			resources: []daemon.ResourceStatus{{Name: "db", State: "healthy"}, {Name: "api", State: "degraded", StateReason: "exited with code 1"}, {Name: "worker", State: "pending"}, {Name: "unwatched", State: "degraded", StateReason: "not requested"}},
			logs:      map[string][]string{"api": {"panic: connection refused", "exit status 1"}},
			want:      []map[string]any{{"name": "api", "kind": "", "state": "degraded", "state_reason": "exited with code 1", "log_tail": []any{"panic: connection refused", "exit status 1"}}, {"name": "worker", "kind": "", "state": "pending"}}},
		{name: "exited infra and dependent retain root cause", affected: []string{"db", "api", "worker"},
			resources: []daemon.ResourceStatus{{Name: "db", Kind: daemon.ResourceKindContainer, Role: "infra", State: "degraded", StateReason: "container exited unexpectedly", FailureKind: "runtime", ExitCode: &exitCode, OOMKilled: true}, {Name: "api", Kind: daemon.ResourceKindService, Role: "backend", State: "pending", PendingDependencies: []string{"db"}}, {Name: "worker", State: "building"}},
			want:      []map[string]any{{"name": "db", "kind": "container", "role": "infra", "state": "degraded", "state_reason": "container exited unexpectedly", "failure_kind": "runtime", "exit_code": float64(137), "oom_killed": true}, {"name": "api", "kind": "service", "role": "backend", "state": "pending", "state_reason": "waiting for db"}, {"name": "worker", "kind": "", "state": "building"}}},
		{name: "health probe supplies missing reason", affected: []string{"api"},
			resources: []daemon.ResourceStatus{{Name: "api", State: "degraded", HealthProgress: &daemon.HealthProgressInfo{LastErr: "connection refused"}}},
			want:      []map[string]any{{"name": "api", "kind": "", "state": "degraded", "state_reason": "connection refused"}}},
		{name: "status unavailable preserves requested resources", affected: []string{"api"}, missingStatus: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepareLifecycleProgressTest(t)
			t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
			if err := os.WriteFile(configFile, []byte("version: \"3\"\nservices:\n  api:\n    command: echo api\n  worker:\n    command: echo worker\ncontainers:\n  db:\n    image: postgres:16\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var started atomic.Bool
			serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/health":
					w.WriteHeader(http.StatusOK)
				case r.URL.Path == "/api/status":
					if started.Load() && tc.missingStatus {
						http.Error(w, "status unavailable", http.StatusServiceUnavailable)
						return
					}
					status := daemon.StatusResponse{ConfigPath: configFile}
					if started.Load() {
						status.Resources = tc.resources
					}
					daemon.WriteJSON(w, http.StatusOK, status)
				case r.URL.Path == "/api/up":
					started.Store(true)
					daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, AffectedResources: tc.affected})
				case strings.HasPrefix(r.URL.Path, "/api/logs/"):
					name := strings.TrimPrefix(r.URL.Path, "/api/logs/")
					daemon.WriteJSON(w, http.StatusOK, daemon.LogsResponse{Lines: tc.logs[name]})
				case r.URL.Path == "/api/history/cli-event":
					w.WriteHeader(http.StatusOK)
				default:
					http.NotFound(w, r)
				}
			})
			out, diagnostics, err := executeAppCommand(t, "--config", configFile, "up", "--json", "--timeout", "3s")
			if err == nil || !started.Load() {
				t.Fatalf("up was not dispatched and failed as expected: %v\n%s\n%s", err, out, diagnostics)
			}
			assertOneLifecycleEnvelope(t, out, false)
			var envelope struct {
				Error *struct {
					Code string `json:"code"`
				} `json:"error"`
				Data struct {
					Operation string           `json:"operation"`
					Requested []string         `json:"requested_resources"`
					Failed    []map[string]any `json:"failed_resources"`
				} `json:"data"`
			}
			if err := json.Unmarshal(out, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil || envelope.Error.Code == "" || envelope.Data.Operation != "up" || !reflect.DeepEqual(envelope.Data.Requested, tc.affected) || !reflect.DeepEqual(envelope.Data.Failed, tc.want) {
				t.Fatalf("lost failure evidence: %s\nwant failed resources: %+v", out, tc.want)
			}
			if strings.Count(diagnostics, "collecting failure evidence") > 1 {
				t.Fatalf("duplicate evidence phase: %s", diagnostics)
			}
		})
	}
}

func TestRunningResourceNamesIgnoresStoppedAndPending(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "db", State: "healthy"},
		{Name: "api", State: "starting"},
		{Name: "worker", State: "stopped"},
		{Name: "queued", State: "pending"},
		{Name: "flaky", State: "degraded"},
	}}
	got := runningResourceNames(status)
	want := []string{"db", "api", "flaky"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("running resources = %v, want %v", got, want)
	}
}
