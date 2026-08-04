package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/internal/envsource"
)

func TestHandleEnvsReportsProjectContextAndInactiveManagedSelection(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	home := OrbitDir()
	registry, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "default", Type: envsource.TypeGit, URL: "https://example.com/envs.git"}, false); err != nil {
		t.Fatal(err)
	}
	managedDir := envsource.EnvsDir(home, "default")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(managedDir, "development.yaml")
	projectRoot := filepath.Join(t.TempDir(), "payments")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(projectRoot, "orbit.yaml")
	for _, path := range []string{managed, project} {
		if err := os.WriteFile(path, []byte("version: \"3\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	managed = canonicalEnvironmentPath(managed)
	project = canonicalEnvironmentPath(project)
	projectRoot = filepath.Dir(project)
	if err := os.WriteFile(CurrentEnvPath(), []byte(managed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv.SetConfigPath(project)
	srv.SetEnvironmentContext(project, "project")
	req := httptest.NewRequest(http.MethodGet, "/api/envs", nil)
	w := httptest.NewRecorder()
	srv.handleEnvs(w, req)

	var response EnvsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Context.Kind != "project" ||
		response.Context.Identity != project ||
		response.Context.ProjectRoot != projectRoot ||
		response.Context.DisplayName != "payments" {
		t.Fatalf("context = %+v", response.Context)
	}
	if response.Context.ManagedSelection == nil ||
		response.Context.ManagedSelection.Path != managed ||
		response.Context.ManagedSelection.Active {
		t.Fatalf("managed selection = %+v", response.Context.ManagedSelection)
	}
	if len(response.Sources) != 1 || len(response.Sources[0].Environments) != 1 ||
		response.Sources[0].Environments[0].Identity != "default/development" || !response.Sources[0].Environments[0].Selected {
		t.Fatalf("managed sources = %+v", response.Sources)
	}
}

func TestEnvironmentContextMarksMissingProjectUnavailable(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	missing := filepath.Join(t.TempDir(), "removed", "orbit.yaml")
	srv.SetConfigPath(missing)
	srv.SetEnvironmentContext(missing, "project")

	context := srv.environmentContext()
	if context.Available || context.Kind != "project" || context.ConfigPath != missing {
		t.Fatalf("context = %+v", context)
	}
}

func TestEnvironmentContextMarksInvalidProjectUnavailable(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	invalid := filepath.Join(t.TempDir(), "orbit.yaml")
	if err := os.WriteFile(invalid, []byte("version: [invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.SetConfigPath(invalid)
	srv.SetEnvironmentContext(invalid, "project")

	if context := srv.environmentContext(); context.Available {
		t.Fatalf("invalid context reported available: %+v", context)
	}
}

func TestEnvironmentContextIdentitySurvivesDeletedSymlink(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	root := t.TempDir()
	target := filepath.Join(root, "project.yaml")
	link := filepath.Join(root, "orbit.yaml")
	if err := os.WriteFile(target, []byte("version: \"3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	srv.SetConfigPath(link)
	srv.SetEnvironmentContext(link, "project")
	before := srv.environmentContext()
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	after := srv.environmentContext()

	if before.Identity != after.Identity {
		t.Fatalf("identity changed after deletion: before=%q after=%q", before.Identity, after.Identity)
	}
	if after.Available {
		t.Fatalf("deleted symlink target reported available: %+v", after)
	}
}

func TestHandleEnvSwitch_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	envsDir := filepath.Join(tmp, "envs")
	_ = os.MkdirAll(envsDir, 0755)
	// Write a minimal but valid env yaml
	target := filepath.Join(envsDir, "newenv.yaml")
	if err := os.WriteFile(target, []byte("version: \"3\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Also write the "current" env so dirname matches
	currentEnv := filepath.Join(envsDir, "current.yaml")
	_ = os.WriteFile(currentEnv, []byte("version: \"3\"\n"), 0644)

	srv := newTestServer(t, &config.Config{})
	srv.SetConfigPath(currentEnv)
	var restartedWith, restartedKind string
	srv.SetRestartLauncher(func(path, kind string) error {
		restartedWith = path
		restartedKind = kind
		return nil
	})

	body := strings.NewReader(`{"env":"newenv"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/env", body)
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if srv.ConfigPath() != target {
		t.Errorf("ConfigPath = %s, want %s", srv.ConfigPath(), target)
	}
	if restartedWith != target {
		t.Errorf("restart target = %q, want %q", restartedWith, target)
	}
	if restartedKind != "managed" {
		t.Errorf("restart context kind = %q, want managed", restartedKind)
	}
	if strings.Contains(w.Body.String(), "run `orbit daemon restart`") {
		t.Fatalf("response leaked a manual daemon step: %s", w.Body.String())
	}
}

func TestHandleEnvSwitch_UnknownEnv(t *testing.T) {
	tmp := t.TempDir()
	envsDir := filepath.Join(tmp, "envs")
	_ = os.MkdirAll(envsDir, 0755)
	current := filepath.Join(envsDir, "current.yaml")
	_ = os.WriteFile(current, []byte("version: \"3\"\n"), 0644)

	srv := newTestServer(t, &config.Config{})
	srv.SetConfigPath(current)

	body := strings.NewReader(`{"env":"does-not-exist"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/env", body)
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleEnvSwitchRequiresServerConfirmationForRunningContext(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current.yaml")
	target := filepath.Join(root, "target.yaml")
	for _, path := range []string{current, target} {
		if err := os.WriteFile(path, []byte("version: \"3\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	srv := newTestServer(t, &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis", Image: "redis:7"},
		},
	})
	srv.SetEnvironmentContext(current, "project")
	srv.app.Orchestrator.OnContainerSeen("redis", true)
	launched := false
	srv.SetRestartLauncher(func(string, string) error {
		launched = true
		return nil
	})

	req := httptest.NewRequest(http.MethodPut, "/api/envs/current", strings.NewReader(`{"env":"target"}`))
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", w.Code, w.Body.String())
	}
	var response EnvironmentSwitchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.ConfirmationRequired ||
		response.CurrentContext == nil ||
		response.CurrentContext.Kind != "project" ||
		response.TargetContext == nil ||
		response.TargetContext.DisplayName != "target" ||
		len(response.RunningResources) != 1 ||
		response.RunningResources[0] != "redis" {
		t.Fatalf("confirmation response = %+v", response)
	}
	if launched || srv.ConfigPath() != current {
		t.Fatalf("unconfirmed switch mutated state: launched=%v path=%q", launched, srv.ConfigPath())
	}

	staleConfirmation := fmt.Sprintf(
		`{"env":"target","confirmed":true,"current_identity":%q,"target_identity":"/different/target.yaml"}`,
		response.CurrentContext.Identity,
	)
	req = httptest.NewRequest(http.MethodPut, "/api/envs/current", strings.NewReader(staleConfirmation))
	w = httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)
	if w.Code != http.StatusConflict || launched || srv.ConfigPath() != current {
		t.Fatalf("stale confirmation mutated state: status=%d launched=%v path=%q", w.Code, launched, srv.ConfigPath())
	}

	staleResources := fmt.Sprintf(
		`{"env":"target","confirmed":true,"current_identity":%q,"target_identity":%q,"running_resources":[]}`,
		response.CurrentContext.Identity,
		response.TargetContext.Identity,
	)
	req = httptest.NewRequest(http.MethodPut, "/api/envs/current", strings.NewReader(staleResources))
	w = httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)
	if w.Code != http.StatusConflict || launched || srv.ConfigPath() != current {
		t.Fatalf("resource-stale confirmation mutated state: status=%d launched=%v path=%q", w.Code, launched, srv.ConfigPath())
	}
}

func TestHandleEnvSwitch_WrongMethod(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/env", nil)
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleEnvSwitch_NewPath(t *testing.T) {
	// Verify the new canonical path /api/envs/current works identically to
	// the deprecated /api/env alias.
	tmp := t.TempDir()
	envsDir := filepath.Join(tmp, "envs")
	_ = os.MkdirAll(envsDir, 0755)
	target := filepath.Join(envsDir, "newenv.yaml")
	if err := os.WriteFile(target, []byte("version: \"3\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	currentEnv := filepath.Join(envsDir, "current.yaml")
	_ = os.WriteFile(currentEnv, []byte("version: \"3\"\n"), 0644)

	srv := newTestServer(t, &config.Config{})
	srv.SetConfigPath(currentEnv)
	srv.SetRestartLauncher(func(string, string) error { return nil })

	body := strings.NewReader(`{"env":"newenv"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/envs/current", body)
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if srv.ConfigPath() != target {
		t.Errorf("ConfigPath = %s, want %s", srv.ConfigPath(), target)
	}
}

func TestHandleEnvSwitchRequiresRestartLauncherBeforeMutation(t *testing.T) {
	tmp := t.TempDir()
	current := filepath.Join(tmp, "current.yaml")
	target := filepath.Join(tmp, "target.yaml")
	for _, path := range []string{current, target} {
		if err := os.WriteFile(path, []byte("version: \"3\"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	srv := newTestServer(t, &config.Config{})
	srv.SetConfigPath(current)
	req := httptest.NewRequest(http.MethodPut, "/api/envs/current", strings.NewReader(`{"env":"target"}`))
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body=%s", w.Code, w.Body.String())
	}
	if srv.ConfigPath() != current {
		t.Fatalf("ConfigPath mutated to %q, want %q", srv.ConfigPath(), current)
	}
}

func TestHandleEnvSwitchRestoresSourceEnvironmentWhenRestartUnavailable(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	home := OrbitDir()
	currentDir := envsource.EnvsDir(home, "current-source")
	targetDir := envsource.EnvsDir(home, "target-source")
	for _, directory := range []string{currentDir, targetDir} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	current := filepath.Join(currentDir, "dev.yaml")
	target := filepath.Join(targetDir, "dev.yaml")
	for _, path := range []string{current, target} {
		if err := os.WriteFile(path, []byte("version: \"3\"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "current-source", Type: envsource.TypeLocal, Path: t.TempDir(), Workspace: "/previous"}, true); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "target-source", Type: envsource.TypeLocal, Path: t.TempDir(), Workspace: "/target"}, false); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKSPACE_ROOT", "/previous")
	t.Setenv("ORBIT_SOURCE_NAME", "current-source")

	srv.SetConfigPath(current)
	req := httptest.NewRequest(http.MethodPut, "/api/envs/current", strings.NewReader(`{"env":"target-source/dev"}`))
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body=%s", w.Code, w.Body.String())
	}
	if got := os.Getenv("WORKSPACE_ROOT"); got != "/previous" {
		t.Fatalf("WORKSPACE_ROOT = %q, want previous value", got)
	}
	if got := os.Getenv("ORBIT_SOURCE_NAME"); got != "current-source" {
		t.Fatalf("ORBIT_SOURCE_NAME = %q, want current-source", got)
	}
}

func TestHandleEnvSwitchRollsBackWhenRestartCannotLaunch(t *testing.T) {
	tmp := t.TempDir()
	current := filepath.Join(tmp, "current.yaml")
	target := filepath.Join(tmp, "target.yaml")
	for _, path := range []string{current, target} {
		if err := os.WriteFile(path, []byte("version: \"3\"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	srv := newTestServer(t, &config.Config{Containers: map[string]*config.Container{
		"redis": {Name: "redis", Image: "redis:7"},
	}})
	srv.SetConfigPath(current)
	srv.app.Orchestrator.OnContainerSeen("redis", true)
	if err := os.WriteFile(CurrentEnvPath(), []byte(current+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.SetRestartLauncher(func(string, string) error { return errors.New("launcher unavailable") })
	currentIdentity := canonicalEnvironmentPath(current)
	targetIdentity := canonicalEnvironmentPath(target)
	body := fmt.Sprintf(
		`{"env":"target","confirmed":true,"current_identity":%q,"target_identity":%q,"running_resources":["redis"]}`,
		currentIdentity,
		targetIdentity,
	)
	req := httptest.NewRequest(http.MethodPut, "/api/envs/current", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", w.Code, w.Body.String())
	}
	if srv.ConfigPath() != current {
		t.Fatalf("ConfigPath = %q, want rollback to %q", srv.ConfigPath(), current)
	}
	if selected := ReadCurrentEnv(); selected != current {
		t.Fatalf("selected env = %q, want rollback to %q", selected, current)
	}
	if stale, reason := srv.configStale(); stale {
		t.Fatalf("rolled-back server stayed stale: %s", reason)
	}
	info, ok := srv.app.Orchestrator.GetServiceInfo("redis")
	if !ok || info.State != engine.StatePending {
		t.Fatalf("redis state after rollback = %+v, want pending running intent", info)
	}
}

func TestHandleEnvSwitchFromProjectRestoresManagedSelectionOnLaunchFailure(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	envsDir := filepath.Join(OrbitDir(), "envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(envsDir, "managed.yaml")
	project := filepath.Join(t.TempDir(), "orbit.yaml")
	for _, path := range []string{managed, project} {
		if err := os.WriteFile(path, []byte("version: \"3\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(CurrentEnvPath(), []byte(managed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.SetConfigPath(project)
	srv.SetEnvironmentContext(project, "project")
	srv.SetRestartLauncher(func(string, string) error { return errors.New("launcher unavailable") })

	req := httptest.NewRequest(http.MethodPut, "/api/envs/current", strings.NewReader(`{"env":"managed"}`))
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", w.Code, w.Body.String())
	}
	if selected := canonicalEnvironmentPath(ReadCurrentEnv()); selected != canonicalEnvironmentPath(managed) {
		t.Fatalf("managed selection = %q, want %q", selected, managed)
	}
	if context := srv.environmentContext(); context.ConfigPath != project || context.Kind != "project" {
		t.Fatalf("project context changed: %+v", context)
	}
}

func TestHandleEnvSwitch_InvalidJSON(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest(http.MethodPut, "/api/env", body)
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
