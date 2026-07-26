package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsEnvToggleOn(t *testing.T) {
	t.Run("nil toggles default true", func(t *testing.T) {
		s := LoadSettings(filepath.Join(t.TempDir(), "s.json"))
		if got := s.IsEnvToggleOn("foo", true); !got {
			t.Error("expected true with nil toggles and default true")
		}
	})
	t.Run("nil toggles default false", func(t *testing.T) {
		s := LoadSettings(filepath.Join(t.TempDir(), "s.json"))
		if got := s.IsEnvToggleOn("foo", false); got {
			t.Error("expected false with nil toggles and default false")
		}
	})
	t.Run("toggle true overrides default false", func(t *testing.T) {
		s := LoadSettings(filepath.Join(t.TempDir(), "s.json"))
		if err := s.SetEnvToggle("foo", true); err != nil {
			t.Fatalf("set toggle: %v", err)
		}
		if got := s.IsEnvToggleOn("foo", false); !got {
			t.Error("expected true when toggle is true")
		}
	})
	t.Run("toggle false overrides default true", func(t *testing.T) {
		s := LoadSettings(filepath.Join(t.TempDir(), "s.json"))
		if err := s.SetEnvToggle("foo", false); err != nil {
			t.Fatalf("set toggle: %v", err)
		}
		if got := s.IsEnvToggleOn("foo", true); got {
			t.Error("expected false when toggle is false")
		}
	})
}

func TestLoadSettings_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0644); err != nil {
		t.Fatal(err)
	}
	s := LoadSettings(path)
	if s == nil {
		t.Fatal("LoadSettings returned nil")
	}
	if s.path != path {
		t.Errorf("path = %q, want %q", s.path, path)
	}
	// Default behavior: no crash, ready to Save()
}

func TestSettings_UserEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	s := LoadSettings(path)
	s.mu.Lock()
	s.UserEnv = map[string]string{
		"PAYMENTS_ROOT": "/test/development",
		"CUSTOM_VAR":    "hello",
	}
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		t.Fatalf("save failed: %v", err)
	}
	s.mu.Unlock()

	// Reload and verify persisted
	s2 := LoadSettings(path)
	if s2.UserEnv["PAYMENTS_ROOT"] != "/test/development" {
		t.Errorf("expected /test/development, got %s", s2.UserEnv["PAYMENTS_ROOT"])
	}

	// ApplyToEnv sets them
	s2.ApplyToEnv()
	if got := os.Getenv("PAYMENTS_ROOT"); got != "/test/development" {
		t.Errorf("expected /test/development in env, got %s", got)
	}
	if got := os.Getenv("CUSTOM_VAR"); got != "hello" {
		t.Errorf("expected hello in env, got %s", got)
	}

	_ = os.Unsetenv("PAYMENTS_ROOT")
	_ = os.Unsetenv("CUSTOM_VAR")
}

func TestSettings_WorkspaceRoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	s := LoadSettings(path)
	if err := s.Set("workspace_root", "/test/workspace"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	// Reload
	s2 := LoadSettings(path)
	if s2.Get("workspace_root") != "/test/workspace" {
		t.Errorf("expected /test/workspace, got %s", s2.Get("workspace_root"))
	}

	// ApplyToEnv
	s2.ApplyToEnv()
	if got := os.Getenv("WORKSPACE_ROOT"); got != "/test/workspace" {
		t.Errorf("expected /test/workspace in WORKSPACE_ROOT, got %s", got)
	}

	_ = os.Unsetenv("WORKSPACE_ROOT")
}

// Regression: env_repo_url used to be missing from the Get/Set switches, so
// `orbit env sync --url` silently never persisted (Get returned "" forever
// and every run fell back to the built-in default).
func TestSettings_EnvRepoURL_Persists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	s := LoadSettings(path)
	if err := s.Set("env_repo_url", "http://example.com/envs.git"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	s2 := LoadSettings(path)
	if got := s2.Get("env_repo_url"); got != "http://example.com/envs.git" {
		t.Errorf("after reload, env_repo_url = %q, want the set URL", got)
	}
}

func TestSettings_SetUnknownKeyErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	s := LoadSettings(path)
	if err := s.Set("no_such_key", "x"); err == nil {
		t.Error("Set with unknown key should error, got nil")
	}
}

func TestSetServiceMode_PersistsToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	s := LoadSettings(path)

	if err := s.SetServiceMode("api", "container"); err != nil {
		t.Fatalf("SetServiceMode: %v", err)
	}

	// Reload from disk — the mode should persist without an explicit Save().
	s2 := LoadSettings(path)
	if got := s2.GetServiceMode("api"); got != "container" {
		t.Errorf("after reload, mode = %q, want %q", got, "container")
	}
}

func TestSettings_DetachedEdges_CRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	s := LoadSettings(path)

	if s.IsEdgeDetached("development", "frontend", "api") {
		t.Fatal("should default to not detached")
	}

	if err := s.SetEdgeDetached("development", "frontend", "api", true); err != nil {
		t.Fatalf("SetEdgeDetached: %v", err)
	}
	if !s.IsEdgeDetached("development", "frontend", "api") {
		t.Fatal("should be detached after Set true")
	}

	// Persists across reload
	s2 := LoadSettings(path)
	if !s2.IsEdgeDetached("development", "frontend", "api") {
		t.Fatal("detached state should persist across reload")
	}

	// Toggle back off
	if err := s2.SetEdgeDetached("development", "frontend", "api", false); err != nil {
		t.Fatalf("SetEdgeDetached false: %v", err)
	}
	if s2.IsEdgeDetached("development", "frontend", "api") {
		t.Fatal("should not be detached after Set false")
	}

	// Empty map cleaned up
	s3 := LoadSettings(path)
	got := s3.GetDetachedEdges("development")
	if len(got) != 0 {
		t.Errorf("GetDetachedEdges = %v, want empty", got)
	}
}

func TestSettings_DetachedEdges_EnvIsolation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	s := LoadSettings(path)

	_ = s.SetEdgeDetached("development", "frontend", "api", true)
	_ = s.SetEdgeDetached("staging", "frontend", "api", false)

	if !s.IsEdgeDetached("development", "frontend", "api") {
		t.Error("development/frontend→api should be detached")
	}
	if s.IsEdgeDetached("staging", "frontend", "api") {
		t.Error("staging/frontend→api should not be detached")
	}
}

func TestSettings_GetDetachedEdges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	s := LoadSettings(path)

	_ = s.SetEdgeDetached("development", "frontend", "api", true)
	_ = s.SetEdgeDetached("development", "frontend", "notifications", true)
	_ = s.SetEdgeDetached("staging", "payments", "worker", true)

	got := s.GetDetachedEdges("development")
	if len(got) != 1 || len(got["frontend"]) != 2 {
		t.Errorf("GetDetachedEdges(development) = %v, want 1 entry with 2 deps", got)
	}
}

func TestSettings_DetachedEdges_MigratesLegacyFlatKeys(t *testing.T) {
	// Write a v1 flat-key settings.json and verify LoadSettings migrates it.
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	legacy := `{
		"detached_edges": {
			"development/frontend": ["api","notifications"],
			"staging/payments": ["worker"]
		}
	}`
	if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s := LoadSettings(path)
	if !s.IsEdgeDetached("development", "frontend", "api") {
		t.Error("development/frontend→api should be migrated as detached")
	}
	if !s.IsEdgeDetached("development", "frontend", "notifications") {
		t.Error("development/frontend→notifications should be migrated as detached")
	}
	if !s.IsEdgeDetached("staging", "payments", "worker") {
		t.Error("staging/payments→worker should be migrated as detached")
	}

	// Save should write the new nested format — reload must still work.
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	s2 := LoadSettings(path)
	if !s2.IsEdgeDetached("development", "frontend", "api") {
		t.Error("migrated state should survive round-trip save/load")
	}
}

func TestSaveLocked_Atomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	s := LoadSettings(path)
	// Seed a value and save.
	if err := s.SetServiceMode("api", "container"); err != nil {
		t.Fatal(err)
	}
	// There should be no stray .tmp file after a successful save.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected .tmp file cleaned up, stat error = %v", err)
	}
	// And the real file is valid JSON with the value.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"container"`) {
		t.Errorf("settings.json does not contain written value: %s", data)
	}
}
