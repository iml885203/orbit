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

	var found *ResourceStatus
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

func TestHandleStatus_ReportsBufferedLogsWithoutGuessingFromLifecycleState(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"api": {Name: "api", Type: "node"},
		},
		Containers: map[string]*config.Container{},
	}
	srv := newTestServer(t, cfg)

	statuses := srv.computeStatuses(cfg)
	if len(statuses) != 1 || statuses[0].LogsAvailable {
		t.Fatalf("fresh service logs_available = %v, want false", statuses)
	}

	srv.app.Logs.Write("api", "server listening")
	statuses = srv.computeStatuses(cfg)
	if len(statuses) != 1 || !statuses[0].LogsAvailable {
		t.Fatalf("buffered service logs_available = %v, want true", statuses)
	}
}

func TestApplyDependencyImpact_PropagatesAndRecoversWithoutChangingRuntime(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"inventory": {Name: "inventory"},
		},
		Services: map[string]*config.Service{
			"orders": {Name: "orders", DependsOn: []string{"inventory"}},
			"shop":   {Name: "shop", DependsOn: []string{"orders"}},
		},
	}
	raw := []ResourceStatus{
		{Name: "inventory", State: "stopped"},
		{Name: "orders", State: "healthy"},
		{Name: "shop", State: "healthy"},
	}

	applyDependencyImpact(cfg, raw)
	byName := make(map[string]ResourceStatus, len(raw))
	for _, status := range raw {
		byName[status.Name] = status
	}
	if got := byName["orders"]; got.State != "degraded" || got.BlockedBy != "inventory" {
		t.Fatalf("orders = %+v, want degraded and blocked by inventory", got)
	}
	if got := byName["shop"]; got.State != "degraded" || got.BlockedBy != "orders" {
		t.Fatalf("shop = %+v, want degraded and blocked by orders", got)
	}

	// The dependent's own runtime probe will eventually fail too. That
	// downstream symptom must not replace the known dependency root cause.
	afterThreshold := []ResourceStatus{
		{Name: "inventory", State: "stopped"},
		{Name: "orders", State: "degraded", StateReason: `Get "http://localhost:3000/health": EOF`},
		{Name: "shop", State: "healthy"},
	}
	applyDependencyImpact(cfg, afterThreshold)
	byName = make(map[string]ResourceStatus, len(afterThreshold))
	for _, status := range afterThreshold {
		byName[status.Name] = status
	}
	if got := byName["orders"]; got.BlockedBy != "inventory" || got.StateReason != "dependency inventory is stopped" {
		t.Fatalf("orders after threshold = %+v, want inventory root cause preserved", got)
	}
	if got := byName["shop"]; got.BlockedBy != "orders" {
		t.Fatalf("shop after threshold = %+v, want direct dependency chain", got)
	}

	recovered := []ResourceStatus{
		{Name: "inventory", State: "healthy"},
		{Name: "orders", State: "healthy"},
		{Name: "shop", State: "healthy"},
	}
	applyDependencyImpact(cfg, recovered)
	for _, status := range recovered {
		if status.State != "healthy" || status.BlockedBy != "" {
			t.Fatalf("recovered %s = %+v, want healthy without blocker", status.Name, status)
		}
	}
}
