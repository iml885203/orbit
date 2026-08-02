package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestHandleEnvsReportsProjectContextAndInactiveManagedSelection(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	home := OrbitDir()
	managedDir := filepath.Join(home, "envs")
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
	srv.SetEnvironmentContextKind("project")
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
	if len(response.Envs) != 1 || response.Envs[0].Path != managed || response.Envs[0].Current {
		t.Fatalf("managed environments = %+v", response.Envs)
	}
}

func TestEnvironmentContextMarksMissingProjectUnavailable(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	missing := filepath.Join(t.TempDir(), "removed", "orbit.yaml")
	srv.SetConfigPath(missing)
	srv.SetEnvironmentContextKind("project")

	context := srv.environmentContext()
	if context.Available || context.Kind != "project" || context.ConfigPath != missing {
		t.Fatalf("context = %+v", context)
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
	var restartedWith string
	srv.SetRestartLauncher(func(path string) error {
		restartedWith = path
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
	srv.SetRestartLauncher(func(string) error { return nil })

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

func TestHandleEnvSwitchRollsBackWhenRestartCannotLaunch(t *testing.T) {
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
	if err := os.WriteFile(CurrentEnvPath(), []byte(current+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.SetRestartLauncher(func(string) error { return errors.New("launcher unavailable") })
	req := httptest.NewRequest(http.MethodPut, "/api/envs/current", strings.NewReader(`{"env":"target"}`))
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
	srv.SetEnvironmentContextKind("project")
	srv.SetRestartLauncher(func(string) error { return errors.New("launcher unavailable") })

	req := httptest.NewRequest(http.MethodPut, "/api/envs/current", strings.NewReader(`{"env":"managed"}`))
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", w.Code, w.Body.String())
	}
	if selected := canonicalEnvironmentPath(ReadCurrentEnv()); selected != canonicalEnvironmentPath(managed) {
		t.Fatalf("managed selection = %q, want %q", selected, managed)
	}
	if srv.ConfigPath() != project || srv.EnvironmentContextKind() != "project" {
		t.Fatalf("project context changed: path=%q kind=%q", srv.ConfigPath(), srv.EnvironmentContextKind())
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
