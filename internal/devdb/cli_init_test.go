package devdb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/daemon"
)

// --yes with nothing detected must be a clean no-op: no prompts (the
// prompt func is nil-equivalent), no settings writes, no error.
func TestInitSteps_YesWithNothingDetected(t *testing.T) {
	t.Setenv("ORBIT_DB_ROOT", "")
	path := filepath.Join(t.TempDir(), "settings.json")
	settings := daemon.LoadSettings(path)
	err := InitSteps(settings, true, func(string) string {
		t.Fatal("prompt called despite --yes")
		return ""
	}, false)
	if err != nil {
		t.Fatalf("InitSteps: %v", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("settings file written despite no values to save")
	}
}

// A detected env value flows through to the setting under --yes.
func TestInitSteps_YesPersistsDetectedDBRoot(t *testing.T) {
	dbRoot := t.TempDir()
	t.Setenv("ORBIT_DB_ROOT", dbRoot)
	settings := daemon.LoadSettings(filepath.Join(t.TempDir(), "settings.json"))
	if err := InitSteps(settings, true, func(string) string { return "" }, false); err != nil {
		t.Fatalf("InitSteps: %v", err)
	}
	if got := settings.Get("db_root"); got != dbRoot {
		t.Errorf("db_root = %q, want %q", got, dbRoot)
	}
}
