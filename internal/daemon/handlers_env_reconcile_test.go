package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/engine"
)

func TestHandleEnvironmentReconcileAddsResourceWithoutInterruptingRunningState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orbit.yaml")
	original := []byte("version: \"3\"\ncontainers:\n  redis:\n    image: redis:7\nservices:\n  api:\n    command: api\n    depends_on: [redis]\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("write original config: %v", err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("load original config: %v", err)
	}
	srv := newTestServer(t, loaded)
	srv.SetConfigPath(path)
	srv.app.Orchestrator.MarkServiceHealthy("redis")
	srv.app.Orchestrator.MarkServiceHealthy("api")

	updated := append(original, []byte("  worker:\n    command: worker\n")...)
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		t.Fatalf("write updated config: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/env/reconcile", nil)
	srv.handleEnvironmentReconcile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	for _, name := range []string{"redis", "api"} {
		info, exists := srv.app.Orchestrator.GetServiceInfo(name)
		if !exists || info.State != engine.StateHealthy {
			t.Fatalf("%s = %+v, exists=%v", name, info, exists)
		}
	}
	worker, exists := srv.app.Orchestrator.GetServiceInfo("worker")
	if !exists || worker.State != engine.StateStopped {
		t.Fatalf("worker = %+v, exists=%v", worker, exists)
	}
	if stale, reason := srv.configStale(); stale {
		t.Fatalf("reconciled config remained stale: %s", reason)
	}
}
