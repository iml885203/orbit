package app

import (
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iml885203/orbit/internal/daemon"
)

func TestRootHumanUpReportsCompletionAndFailureEvidence(t *testing.T) {
	for _, outcome := range []string{"healthy", "degraded"} {
		t.Run(outcome, func(t *testing.T) {
			prepareLifecycleProgressTest(t)
			t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
			if err := os.WriteFile(configFile, []byte("version: \"3\"\nservices:\n  worker:\n    command: echo worker\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var started atomic.Bool
			var observations atomic.Int32
			serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/health":
					w.WriteHeader(http.StatusOK)
				case "/api/status":
					resource := daemon.ResourceStatus{Name: "worker", State: "stopped"}
					if started.Load() {
						resource.State = outcome
						if outcome == "healthy" && observations.Add(1) == 1 {
							resource.State = "starting"
						}
						if outcome == "degraded" {
							resource.StateReason = "exited: exit status 1"
						}
					}
					daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{ConfigPath: configFile, Resources: []daemon.ResourceStatus{resource}})
				case "/api/up":
					started.Store(true)
					daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, Message: "Starting worker", AffectedResources: []string{"worker"}})
				case "/api/logs/worker":
					daemon.WriteJSON(w, http.StatusOK, daemon.LogsResponse{Lines: []string{"application failed", "[orbit] exited: exit status 1"}})
				case "/api/history/cli-event":
					w.WriteHeader(http.StatusOK)
				default:
					http.NotFound(w, r)
				}
			})
			out, diagnostics, err := executeAppCommand(t, "--config", configFile, "up", "--timeout", "5s")
			if (err != nil) != (outcome == "degraded") {
				t.Fatalf("up: %v\n%s\n%s", err, out, diagnostics)
			}
			if !started.Load() || strings.Count(string(out), "worker "+outcome+" (") != 1 {
				t.Fatalf("missing or duplicated terminal progress: %s", out)
			}
			if outcome == "healthy" {
				if !strings.Contains(string(out), "Environment is healthy.") || strings.Contains(diagnostics, "Error:") {
					t.Fatalf("completion: %s %s", out, diagnostics)
				}
			} else {
				for _, want := range []string{"worker failed to become healthy", "exited: exit status 1", "application failed", "orbit logs worker"} {
					if !strings.Contains(diagnostics, want) {
						t.Fatalf("failure missing %q: %s", want, diagnostics)
					}
				}
				if strings.Contains(string(out), "Environment is healthy.") {
					t.Fatalf("false success: %s", out)
				}
			}
		})
	}
}
