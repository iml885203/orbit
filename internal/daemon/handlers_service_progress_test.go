package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestHandleStatus_IncludesHealthProgressWhenAvailable(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"worker": {
				Name:        "worker",
				Type:        "node",
				HealthCheck: &config.HealthCheckConfig{Type: "http", Retries: 60},
			},
		},
		Containers: map[string]*config.Container{},
	}
	srv := newTestServer(t, cfg)
	srv.app.HealthChecker.RecordProgressForTest("worker", 9, 60, "connection refused")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var found *ServiceStatus
	for i := range resp.Resources {
		if resp.Resources[i].Name == "worker" {
			found = &resp.Resources[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("worker not in response")
	}
	if found.HealthProgress == nil {
		t.Fatalf("HealthProgress should be populated")
	}
	if found.HealthProgress.Attempts != 9 {
		t.Errorf("Attempts = %d, want 9", found.HealthProgress.Attempts)
	}
	if found.HealthProgress.LastErr != "connection refused" {
		t.Errorf("LastErr = %q, want %q", found.HealthProgress.LastErr, "connection refused")
	}
}

func TestHandleStatus_OmitsHealthProgressWhenServiceHasNoHealthCheck(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"no-hc": {Name: "no-hc", Type: "node"}, // no HealthCheck
		},
		Containers: map[string]*config.Container{},
	}
	srv := newTestServer(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.handleStatus(w, req)

	var resp StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, s := range resp.Resources {
		if s.Name == "no-hc" && s.HealthProgress != nil {
			t.Errorf("HealthProgress should be nil for service without health check, got %+v", s.HealthProgress)
		}
	}
}
