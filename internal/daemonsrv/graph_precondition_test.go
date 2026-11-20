package daemonsrv

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
)

func TestEdgeMutationChecksCapturedEnvironmentBeforeEffects(t *testing.T) {
	for _, scenario := range []struct {
		name              string
		epoch, generation string
		replace           bool
		want              int
	}{
		{name: "matching", want: http.StatusOK},
		{name: "replacement while request waits", replace: true, want: http.StatusConflict},
		{name: "previous daemon", epoch: "1", want: http.StatusConflict},
		{name: "malformed", generation: "invalid", want: http.StatusBadRequest},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			srv := newTestServer(t, &config.Config{Services: map[string]*config.Service{
				"frontend": {Name: "frontend", Kind: "frontend", DependsOn: []string{"api"}},
				"api":      {Name: "api", Kind: "backend"},
			}})
			srv.SetConfigPath(filepath.Join(t.TempDir(), "development.yaml"))
			request := httptest.NewRequest(http.MethodPut, "/api/edges/frontend/api", strings.NewReader(`{"env":"development","detached":true}`))
			epoch := strconv.FormatInt(srv.epoch(), 10)
			generation := strconv.FormatUint(srv.environmentGeneration.Load(), 10)
			if scenario.epoch != "" {
				epoch = scenario.epoch
			}
			if scenario.generation != "" {
				generation = scenario.generation
			}
			request.Header.Set(daemon.ExpectedEpochHeader, epoch)
			request.Header.Set(daemon.ExpectedGenerationHeader, generation)
			result := httptest.NewRecorder()
			if scenario.replace {
				srv.environmentTransitionMu.Lock()
				started := make(chan struct{})
				completed := make(chan struct{})
				go func() {
					close(started)
					srv.handleEdgeDetach(result, request)
					close(completed)
				}()
				<-started
				srv.environmentGeneration.Add(1)
				srv.environmentTransitionMu.Unlock()
				select {
				case <-completed:
				case <-time.After(2 * time.Second):
					t.Fatal("queued graph mutation did not return after transition")
				}
			} else {
				srv.handleEdgeDetach(result, request)
			}
			if result.Code != scenario.want {
				t.Fatalf("HTTP %d, want %d: %s", result.Code, scenario.want, result.Body.String())
			}
			if got := srv.settings.IsEdgeDetached("development", "frontend", "api"); got != (scenario.want == http.StatusOK) {
				t.Fatalf("persisted detached=%v after HTTP %d", got, result.Code)
			}
		})
	}
}
