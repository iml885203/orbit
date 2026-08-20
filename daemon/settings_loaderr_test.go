package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// Settings that exist but cannot be read used to produce the same empty
// snapshot as a clean install. A caller deciding whether a toggle is off
// then reads an absent key and falls back to a default that may not be in
// force — failing open. The two cases must be distinguishable.
func TestLoadSettingsDistinguishesUnreadableFromAbsent(t *testing.T) {
	dir := t.TempDir()

	t.Run("absent settings are not an error", func(t *testing.T) {
		s := LoadSettings(filepath.Join(dir, "does-not-exist.json"))
		if err := s.LoadError(); err != nil {
			t.Errorf("LoadError = %v, want nil for a first run", err)
		}
	})

	t.Run("corrupt settings report the failure", func(t *testing.T) {
		path := filepath.Join(dir, "corrupt.json")
		if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		if LoadSettings(path).LoadError() == nil {
			t.Error("LoadError = nil for unparseable settings, want an error")
		}
	})

	t.Run("unreadable settings report the failure", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads regardless of mode")
		}
		path := filepath.Join(dir, "unreadable.json")
		if err := os.WriteFile(path, []byte(`{"env_toggles":{"a/B":false}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

		if LoadSettings(path).LoadError() == nil {
			t.Error("LoadError = nil for an unreadable file, want an error")
		}
	})

	t.Run("readable settings preserve a false toggle", func(t *testing.T) {
		path := filepath.Join(dir, "good.json")
		if err := os.WriteFile(path, []byte(`{"env_toggles":{"a/B":false}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		s := LoadSettings(path)
		if err := s.LoadError(); err != nil {
			t.Fatalf("LoadError = %v, want nil", err)
		}
		// A toggle explicitly set to false must survive as false, not vanish
		// into the same empty map an unset toggle produces.
		got := s.GetEnvToggles()
		if v, ok := got["a/B"]; !ok || v {
			t.Errorf("toggles = %v, want a/B present and false", got)
		}
	})
}

// The daemon consumes toggles to decide which env vars each service gets.
// Starting from settings it could not read would inject declared defaults in
// place of the user's choices — a wrong environment, reported as a normal
// start. LoadError is what lets the caller refuse instead.
func TestUnreadableSettingsYieldEmptyTogglesAndAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"env_toggles":{"svc/FLAG":false}`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := LoadSettings(path)

	// The dangerous part is that the toggles look like a clean slate...
	if got := s.GetEnvToggles(); len(got) != 0 {
		t.Fatalf("toggles = %v, want empty (the shape a caller must not trust)", got)
	}
	// ...so the error is the only thing distinguishing it from a first run.
	if s.LoadError() == nil {
		t.Error("LoadError = nil, want an error alongside the empty toggles")
	}
}
