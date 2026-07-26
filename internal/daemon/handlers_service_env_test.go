package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestHandleServiceEnv_ReturnsEnvVars(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis", Image: "redis:7", Ports: map[string]config.PortDef{
				"redis": {Host: 6379},
			}},
		},
		Services: map[string]*config.Service{
			"api": {
				Name:      "api",
				DependsOn: []string{"redis"},
				Env:       map[string]string{"APP_PORT": "3000"},
			},
		},
	}
	srv := newTestServer(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/service-env/api", nil)
	req.URL.Path = "/api/service-env/api"
	w := httptest.NewRecorder()
	srv.handleServiceEnv(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var resp ServiceEnvResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Service != "api" {
		t.Errorf("service = %q, want %q", resp.Service, "api")
	}

	byKey := map[string]EnvVarEntry{}
	for _, e := range resp.Env {
		byKey[e.Key] = e
	}

	// Explicit env var should be present with source=explicit
	appPort, ok := byKey["APP_PORT"]
	if !ok {
		t.Fatal("APP_PORT missing from env response")
	}
	if appPort.Source != "explicit" {
		t.Errorf("APP_PORT source = %q, want %q", appPort.Source, "explicit")
	}
	if appPort.Value != "3000" {
		t.Errorf("APP_PORT value = %q, want %q", appPort.Value, "3000")
	}

	// Redis connection string injected via dependency
	connStr, ok := byKey["ConnectionStrings__redis"]
	if !ok {
		t.Fatal("ConnectionStrings__redis missing from env response")
	}
	if connStr.Source != "dependency" {
		t.Errorf("ConnectionStrings__redis source = %q, want %q", connStr.Source, "dependency")
	}
}

func TestHandleServiceEnv_NotFound(t *testing.T) {
	srv := newTestServer(t, testConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/service-env/nonexistent", nil)
	req.URL.Path = "/api/service-env/nonexistent"
	w := httptest.NewRecorder()
	srv.handleServiceEnv(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleServiceEnv_RejectsMissingName(t *testing.T) {
	srv := newTestServer(t, testConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/service-env/", nil)
	req.URL.Path = "/api/service-env/"
	w := httptest.NewRecorder()
	srv.handleServiceEnv(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleServiceEnv_RejectsNonGet(t *testing.T) {
	srv := newTestServer(t, testConfig())

	req := httptest.NewRequest(http.MethodPost, "/api/service-env/api", nil)
	req.URL.Path = "/api/service-env/api"
	w := httptest.NewRecorder()
	srv.handleServiceEnv(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}
