package daemon

import (
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
	if err := os.WriteFile(target, []byte("version: \"2\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Also write the "current" env so dirname matches
	currentEnv := filepath.Join(envsDir, "current.yaml")
	_ = os.WriteFile(currentEnv, []byte("version: \"2\"\n"), 0644)

	srv := newTestServer(t, &config.Config{})
	srv.SetConfigPath(currentEnv)

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
}

func TestHandleEnvSwitch_UnknownEnv(t *testing.T) {
	tmp := t.TempDir()
	envsDir := filepath.Join(tmp, "envs")
	_ = os.MkdirAll(envsDir, 0755)
	current := filepath.Join(envsDir, "current.yaml")
	_ = os.WriteFile(current, []byte("version: \"2\"\n"), 0644)

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
	if err := os.WriteFile(target, []byte("version: \"2\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	currentEnv := filepath.Join(envsDir, "current.yaml")
	_ = os.WriteFile(currentEnv, []byte("version: \"2\"\n"), 0644)

	srv := newTestServer(t, &config.Config{})
	srv.SetConfigPath(currentEnv)

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

func TestHandleEnvSwitch_RejectsPreviewOnly(t *testing.T) {
	tmp := t.TempDir()
	envsDir := filepath.Join(tmp, "envs")
	_ = os.MkdirAll(envsDir, 0755)
	currentEnv := filepath.Join(envsDir, "current.yaml")
	if err := os.WriteFile(currentEnv, []byte("version: \"2\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	prodEnv := filepath.Join(envsDir, "prod.yaml")
	if err := os.WriteFile(prodEnv, []byte("version: \"2\"\npreviewOnly: true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, &config.Config{})
	srv.SetConfigPath(currentEnv)

	body := strings.NewReader(`{"env":"prod"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/env", body)
	w := httptest.NewRecorder()
	srv.handleEnvSwitch(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", w.Code, w.Body.String())
	}
	// Live config path must be untouched — reject happens before any
	// state change so the user isn't stranded with everything stopped.
	if srv.ConfigPath() != currentEnv {
		t.Errorf("ConfigPath mutated to %q; switch should reject before any state change", srv.ConfigPath())
	}
}
