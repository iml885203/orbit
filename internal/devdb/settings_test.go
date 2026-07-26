package devdb

// Settings tests exercising ExampleTeam-owned keys — moved from the daemon
// when the namespace codec took ownership (spec B6).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/daemon"
)

func TestSettings_LoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Set and save
	s := daemon.LoadSettings(path)
	if err := s.Set("sql_server_image", "custom:image"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if err := s.Set("sql_server_pull_policy", "never"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	// Reload and verify the values round-trip through disk
	s2 := daemon.LoadSettings(path)
	if got := s2.Get("sql_server_image"); got != "custom:image" {
		t.Errorf("expected custom:image, got %s", got)
	}
	if got := s2.Get("sql_server_pull_policy"); got != "never" {
		t.Errorf("expected never, got %s", got)
	}

	// Clear and verify empty round-trips
	if err := s2.Set("sql_server_image", ""); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	s3 := daemon.LoadSettings(path)
	if got := s3.Get("sql_server_image"); got != "" {
		t.Errorf("expected empty after clear, got %s", got)
	}
}

func TestSettings_ApplyToEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	s := daemon.LoadSettings(path)
	if err := s.Set("sql_server_image", "test:local"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	s.ApplyToEnv()

	if got := os.Getenv("SQL_SERVER_IMAGE"); got != "test:local" {
		t.Errorf("expected test:local, got %s", got)
	}

	// Cleanup
	_ = os.Unsetenv("SQL_SERVER_IMAGE")
}
