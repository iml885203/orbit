package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTempConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// staleServer returns a Server with just enough state for configStale:
// the baseline is recorded through the production choke point.
func staleServer(t *testing.T, configPath string) *Server {
	t.Helper()
	t.Setenv("ORBIT_HOME", t.TempDir())
	s := &Server{}
	s.SetConfigPath(configPath)
	return s
}

func TestConfigStale_FreshBaselineIsNotStale(t *testing.T) {
	path := writeTempConfig(t, t.TempDir(), "dev.yaml", "version: \"2\"\n")
	s := staleServer(t, path)

	if stale, reason := s.configStale(); stale {
		t.Fatalf("fresh baseline reported stale: %s", reason)
	}
}

func TestConfigStale_FileEdited(t *testing.T) {
	dir := t.TempDir()
	path := writeTempConfig(t, dir, "dev.yaml", "version: \"2\"\n")
	s := staleServer(t, path)

	// Backdate-proof: ensure the mtime actually differs before rewriting.
	time.Sleep(10 * time.Millisecond)
	writeTempConfig(t, dir, "dev.yaml", "version: \"2\"\npreviewOnly: true\n")

	stale, reason := s.configStale()
	if !stale || reason != "env file edited" {
		t.Fatalf("edited file not detected: stale=%v reason=%q", stale, reason)
	}
}

func TestConfigStale_TouchWithoutChangeIsNotStale(t *testing.T) {
	dir := t.TempDir()
	content := "version: \"2\"\n"
	path := writeTempConfig(t, dir, "dev.yaml", content)
	s := staleServer(t, path)

	// Same bytes, new mtime — the hash comparison must absorb the touch.
	time.Sleep(10 * time.Millisecond)
	writeTempConfig(t, dir, "dev.yaml", content)

	if stale, reason := s.configStale(); stale {
		t.Fatalf("touch without change reported stale: %s", reason)
	}
}

func TestConfigStale_SelectionChanged_OnlyForCurrentBackedDaemons(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	dir := t.TempDir()
	loaded := writeTempConfig(t, dir, "development.yaml", "version: \"2\"\n")
	other := writeTempConfig(t, dir, "sports.yaml", "version: \"2\"\n")

	// Daemon started from the current selection.
	if err := os.WriteFile(filepath.Join(home, "current"), []byte(loaded+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetConfigPath(loaded)

	// User switches selection behind the daemon's back (orbit switch).
	if err := os.WriteFile(filepath.Join(home, "current"), []byte(other+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, reason := s.configStale()
	if !stale || reason != "env selection changed" {
		t.Fatalf("selection change not detected: stale=%v reason=%q", stale, reason)
	}

	// A -c daemon (loaded path never matched current) must not false-alarm.
	explicit := &Server{}
	explicit.SetConfigPath(writeTempConfig(t, dir, "custom.yaml", "version: \"2\"\n"))
	if stale, reason := explicit.configStale(); stale {
		t.Fatalf("-c daemon false-alarmed on selection: %s", reason)
	}
}

func TestConfigStale_EngineStaleSticky(t *testing.T) {
	path := writeTempConfig(t, t.TempDir(), "dev.yaml", "version: \"2\"\n")
	s := staleServer(t, path)

	s.engineStale.Store(true)
	stale, reason := s.configStale()
	if !stale || reason != "env switched — restart to rebuild the service graph" {
		t.Fatalf("engine staleness not reported: stale=%v reason=%q", stale, reason)
	}
}
