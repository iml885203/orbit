package daemon

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
)

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
