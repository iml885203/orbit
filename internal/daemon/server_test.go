package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
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
	got := make(map[string]string, len(resp.Resources))
	for _, svc := range resp.Resources {
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
	if got, want := strings.Join(response.AffectedResources, ","), "db,redis"; got != want {
		t.Fatalf("affected services = %q, want %q", got, want)
	}
	if response.Message != "Starting infrastructure (2 containers)." {
		t.Fatalf("message = %q", response.Message)
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
	if response.Message != "No resources are enabled for this environment." {
		t.Fatalf("message = %q", response.Message)
	}
	if len(response.AffectedResources) != 0 {
		t.Fatalf("affected resources = %v, want none", response.AffectedResources)
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
	if got, want := strings.Join(response.AffectedResources, ","), "api,db,redis,web"; got != want {
		t.Fatalf("affected services = %q, want %q", got, want)
	}
	if response.Message != "Starting group frontend (4 resources)." {
		t.Fatalf("message = %q", response.Message)
	}
}

func TestHandleUp_MessageDescribesSelectionIntent(t *testing.T) {
	tests := []struct {
		name    string
		request UpRequest
		want    string
	}{
		{
			name:    "whole environment",
			request: UpRequest{},
			want:    "Starting environment (4 resources).",
		},
		{
			name:    "one container",
			request: UpRequest{Resources: []string{"redis"}},
			want:    "Starting redis.",
		},
		{
			name:    "one service with dependencies",
			request: UpRequest{Resources: []string{"web"}},
			want:    "Starting web with 2 dependencies.",
		},
		{
			name:    "multiple resources with shared dependency",
			request: UpRequest{Resources: []string{"api", "web"}},
			want:    "Starting 2 requested resources with 1 dependency.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, testConfig())
			body, _ := json.Marshal(tt.request)
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/up", bytes.NewReader(body))
			s.handleUp(rr, req)

			var response APIResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Message != tt.want {
				t.Fatalf("message = %q, want %q", response.Message, tt.want)
			}
		})
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

func TestResolveExplicitServices_UnknownResource(t *testing.T) {
	s := newTestServer(t, testConfig())
	_, err := s.resolveExplicitServices([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown resource")
	}
	if !errors.Is(err, errUnknownResource) {
		t.Fatalf("error = %v, want errUnknownResource", err)
	}
	if err.Error() != "unknown resource: nonexistent" {
		t.Errorf("error = %q", err.Error())
	}
}

func TestLifecycleHandlersRejectUnknownResourceSynchronously(t *testing.T) {
	s := newTestServer(t, testConfig())
	tests := []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{name: "stop", path: "/api/stop/nonexistent", handler: s.handleStop},
		{name: "restart", path: "/api/restart/nonexistent", handler: s.handleRestart},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			tt.handler(rr, req)

			assertUnknownResourceResponse(t, rr, "nonexistent")
		})
	}
}

func TestHandleLogsRejectsUnknownResourceBeforeReadingOrStreaming(t *testing.T) {
	s := newTestServer(t, testConfig())
	for _, path := range []string{
		"/api/logs/nonexistent",
		"/api/logs/nonexistent/stream",
	} {
		t.Run(path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			s.handleLogs(rr, req)

			assertUnknownResourceResponse(t, rr, "nonexistent")
		})
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
	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != "resource name required" {
		t.Errorf("error = %q", resp.Error)
	}
}

func assertUnknownResourceResponse(t *testing.T, rr *httptest.ResponseRecorder, name string) {
	t.Helper()
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rr.Code, rr.Body.String())
	}
	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Code != apiCodeUnknownResource {
		t.Errorf("code = %q, want %q", resp.Code, apiCodeUnknownResource)
	}
	if resp.Error != "unknown resource: "+name {
		t.Errorf("error = %q", resp.Error)
	}
}
