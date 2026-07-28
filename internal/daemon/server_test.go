package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/engine"
)

// newTestServer builds a *Server backed by a real *engine.App without a
// running orchestrator. Handlers that only inspect orchestrator state or
// enqueue events (StartServices) work; handlers that require container
// actions (actual Docker) will fail against a missing daemon — tests below
// stick to the former.
func newTestServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
	// Isolate OrbitDir() so handlers that persist into it (e.g. the env-switch
	// "current" pointer) never touch the real ~/.orbit / %LOCALAPPDATA%\orbit.
	// Setting HOME is not enough on Windows, where OrbitDir prefers LOCALAPPDATA.
	t.Setenv("ORBIT_HOME", t.TempDir())
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:1")
	holder := config.NewHolder(cfg)
	app, err := engine.NewApp(holder, nil, nil, "orbit-test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.ContainerMgr.Close() })

	settings := LoadSettings(filepath.Join(t.TempDir(), "settings.json"))
	stateFile := NewStateFile(filepath.Join(t.TempDir(), "state.json"))
	server := NewServer(app, holder, stateFile, settings, "test-version", nil, nil)
	t.Cleanup(server.waitForBackground)
	return server
}

func testConfig() *config.Config {
	return &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis", Image: "redis:7"},
			"db":    {Name: "db", Image: "postgres:15"},
		},
		Services: map[string]*config.Service{
			"api": {Name: "api", Type: "dotnet", DependsOn: []string{"db"}},
			"web": {Name: "web", Type: "node", DependsOn: []string{"api"}},
		},
	}
}

func TestHandleHealth(t *testing.T) {
	s := newTestServer(t, testConfig())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	s.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Errorf("OK = false, want true")
	}
}

func TestHandleVersion(t *testing.T) {
	s := newTestServer(t, testConfig())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	s.handleVersion(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp VersionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Running != "test-version" {
		t.Errorf("Running = %q, want %q", resp.Running, "test-version")
	}
}

func TestHandleVersion_RejectsNonGet(t *testing.T) {
	s := newTestServer(t, testConfig())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/version", nil)
	s.handleVersion(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandleStatus_ListsAllServices(t *testing.T) {
	s := newTestServer(t, testConfig())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	s.handleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp StatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := make(map[string]string, len(resp.Services))
	for _, svc := range resp.Services {
		got[svc.Name] = string(svc.Kind)
	}
	want := map[string]string{
		"redis": "container",
		"db":    "container",
		"api":   "service",
		"web":   "service",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("service %q kind = %q, want %q", name, got[name], kind)
		}
	}
}

func TestHandleStatus_RejectsNonGet(t *testing.T) {
	s := newTestServer(t, testConfig())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	s.handleStatus(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandleUp_InfraOnly(t *testing.T) {
	s := newTestServer(t, testConfig())

	body, _ := json.Marshal(UpRequest{InfraOnly: true})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/up", bytes.NewReader(body))
	s.handleUp(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var response APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, want := strings.Join(response.AffectedServices, ","), "db,redis"; got != want {
		t.Fatalf("affected services = %q, want %q", got, want)
	}

	// Containers should be pending/ready; services should still be stopped.
	infos := s.app.Orchestrator.GetAllServices()
	states := map[string]string{}
	for _, info := range infos {
		states[info.Name] = info.State.String()
	}
	stopped := engine.StateStopped.String()
	for _, c := range []string{"redis", "db"} {
		if states[c] == stopped {
			t.Errorf("container %q state = stopped after InfraOnly up; expected started-or-pending", c)
		}
	}
	for _, svc := range []string{"api", "web"} {
		if states[svc] != stopped {
			t.Errorf("service %q state = %q; expected stopped (InfraOnly should not touch services)", svc, states[svc])
		}
	}
}

func TestHandleUp_EmptyEnvironmentReportsSuccessfulNoOp(t *testing.T) {
	s := newTestServer(t, &config.Config{})

	body, _ := json.Marshal(UpRequest{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/up", bytes.NewReader(body))
	s.handleUp(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var response APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.OK {
		t.Fatalf("response = %+v, want successful no-op", response)
	}
	if response.Message != "No services or containers are enabled for this environment." {
		t.Fatalf("message = %q", response.Message)
	}
	if len(response.AffectedServices) != 0 {
		t.Fatalf("affected services = %v, want none", response.AffectedServices)
	}
}

func TestHandleUp_SelectedGroupReportsActualDependencies(t *testing.T) {
	cfg := testConfig()
	cfg.Groups = map[string]config.Group{
		"frontend": {Services: []string{"web"}},
	}
	s := newTestServer(t, cfg)

	body, _ := json.Marshal(UpRequest{Groups: []string{"frontend"}})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/up", bytes.NewReader(body))
	s.handleUp(rr, req)

	var response APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, want := strings.Join(response.AffectedServices, ","), "api,db,redis,web"; got != want {
		t.Fatalf("affected services = %q, want %q", got, want)
	}
}

func TestHandleUp_RejectsUnknownGroupWithAvailableNames(t *testing.T) {
	cfg := testConfig()
	cfg.Groups = map[string]config.Group{
		"backend":  {Services: []string{"api"}},
		"frontend": {Services: []string{"web"}},
	}
	s := newTestServer(t, cfg)

	body, _ := json.Marshal(UpRequest{Groups: []string{"typo"}})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/up", bytes.NewReader(body))
	s.handleUp(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	var response APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != "unknown_group" {
		t.Fatalf("code = %q, want unknown_group", response.Code)
	}
	if response.Error != "unknown group: typo; available groups: backend, frontend" {
		t.Fatalf("error = %q", response.Error)
	}
}

func TestHandleUp_RejectsNonPost(t *testing.T) {
	s := newTestServer(t, testConfig())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/up", nil)
	s.handleUp(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestResolveExplicitServices_IncludesTransitiveDeps(t *testing.T) {
	s := newTestServer(t, testConfig())

	names, err := s.resolveExplicitServices([]string{"web"})
	if err != nil {
		t.Fatalf("resolveExplicitServices: %v", err)
	}
	sort.Strings(names)
	want := []string{"api", "db", "web"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("resolved = %v, want %v", names, want)
	}
}

func TestResolveExplicitServices_UnknownService(t *testing.T) {
	s := newTestServer(t, testConfig())
	_, err := s.resolveExplicitServices([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error = %q, expected it to mention the bad name", err.Error())
	}
}

func TestHandleStop_AcksUnknownService(t *testing.T) {
	// handleStop is fire-and-forget: it acks the request immediately and
	// runs StopService in a goroutine. Unknown-service failures are
	// logged, not returned over HTTP — the CLI observes them via status
	// polling.
	cfg := testConfig()
	cfg.Settings.ShutdownTimeout = 0 // no wait, orchestrator rejects before any I/O
	s := newTestServer(t, cfg)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/stop/nonexistent", nil)
	s.handleStop(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (handler is non-blocking)", rr.Code)
	}
	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Errorf("OK = false, want true; body = %+v", resp)
	}
}

func TestHandleStop_RequiresName(t *testing.T) {
	s := newTestServer(t, testConfig())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/stop/", nil)
	s.handleStop(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}
