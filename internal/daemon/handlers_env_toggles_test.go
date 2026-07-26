package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
)

// envToggleTestConfig returns a Config with a service that has an EnvToggle
// defined, suitable for testing the PUT /api/env-toggles handler.
func envToggleTestConfig() *config.Config {
	return &config.Config{
		Services: map[string]*config.Service{
			"api": {
				Name: "api",
				Type: "dotnet",
				Env:  map[string]string{"FEATURE_FLAG": "true"},
				EnvToggles: map[string]config.EnvToggle{
					"FEATURE_FLAG": {
						Label:       "Feature flag",
						Description: "Enables the experimental feature",
						Default:     false,
					},
				},
			},
		},
	}
}

// TestEnvToggle_StoppedService_SavesWithoutRestart verifies that toggling an
// env var on a stopped service persists the value but does NOT trigger a
// restart. All services initialise in StateStopped, so no state surgery is
// needed — this is the default condition straight after NewApp.
func TestEnvToggle_StoppedService_SavesWithoutRestart(t *testing.T) {
	s := newTestServer(t, envToggleTestConfig())

	body, _ := json.Marshal(EnvToggleUpdateRequest{
		Service: "api",
		Var:     "FEATURE_FLAG",
		Enabled: true,
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/env-toggles", bytes.NewReader(body))
	s.handleEnvToggles(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}

	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Errorf("OK = false; body = %+v", resp)
	}
	if !strings.Contains(resp.Message, "saved (will apply on next start)") {
		t.Errorf("message = %q, want it to contain %q", resp.Message, "saved (will apply on next start)")
	}
	if strings.Contains(resp.Message, "restarting") {
		t.Errorf("message = %q, must NOT contain %q for a stopped service", resp.Message, "restarting")
	}
}

// TestEnvToggle_StoppedService_ToggleOff_SavesWithoutRestart verifies the
// same no-restart guarantee when disabling a toggle on a stopped service.
func TestEnvToggle_StoppedService_ToggleOff_SavesWithoutRestart(t *testing.T) {
	s := newTestServer(t, envToggleTestConfig())

	// Pre-enable the toggle so disabling it is a meaningful change.
	if err := s.settings.SetEnvToggle("api/FEATURE_FLAG", true); err != nil {
		t.Fatalf("pre-enable toggle: %v", err)
	}

	body, _ := json.Marshal(EnvToggleUpdateRequest{
		Service: "api",
		Var:     "FEATURE_FLAG",
		Enabled: false,
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/env-toggles", bytes.NewReader(body))
	s.handleEnvToggles(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}

	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Errorf("OK = false; body = %+v", resp)
	}
	if !strings.Contains(resp.Message, "saved (will apply on next start)") {
		t.Errorf("message = %q, want %q", resp.Message, "saved (will apply on next start)")
	}
	if strings.Contains(resp.Message, "restarting") {
		t.Errorf("message = %q, must NOT mention restarting for a stopped service", resp.Message)
	}
}

// TestEnvToggle_GET_ReturnsToggles verifies the GET path lists toggles.
func TestEnvToggle_GET_ReturnsToggles(t *testing.T) {
	s := newTestServer(t, envToggleTestConfig())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/env-toggles", nil)
	s.handleEnvToggles(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}

	var toggles []EnvToggleInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &toggles); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(toggles) != 1 {
		t.Fatalf("got %d toggles, want 1", len(toggles))
	}
	got := toggles[0]
	if got.Service != "api" || got.Var != "FEATURE_FLAG" {
		t.Errorf("toggle = %+v, want service=api var=FEATURE_FLAG", got)
	}
	if got.Enabled != false {
		t.Errorf("enabled = %v, want false (default)", got.Enabled)
	}
}

// TestEnvToggle_InvalidJSON_ReturnsBadRequest verifies malformed input is
// rejected cleanly.
func TestEnvToggle_InvalidJSON_ReturnsBadRequest(t *testing.T) {
	s := newTestServer(t, envToggleTestConfig())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/env-toggles", strings.NewReader("{not json"))
	s.handleEnvToggles(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// TestEnvToggle_MethodNotAllowed verifies unsupported HTTP methods get a 405.
func TestEnvToggle_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, envToggleTestConfig())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/env-toggles", nil)
	s.handleEnvToggles(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}
