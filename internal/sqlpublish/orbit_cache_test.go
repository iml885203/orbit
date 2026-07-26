package sqlpublish

import (
	"path/filepath"
	"testing"
)

// orbitCacheDir picks the orbit base dir with a three-way precedence
// (ORBIT_HOME > LOCALAPPDATA > HOME/.orbit) that silently decides where every
// cached dacpac lands — a regression would misdirect the cache with no build
// failure to surface it. t.Setenv isolates the env and t.TempDir keeps the
// MkdirAll off the real home.
func TestOrbitCacheDir_Precedence(t *testing.T) {
	t.Run("ORBIT_HOME wins", func(t *testing.T) {
		base := t.TempDir()
		t.Setenv("ORBIT_HOME", base)
		t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "wrong"))
		got, err := orbitCacheDir("dacpac")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(base, "cache", "dacpac"); got != want {
			t.Errorf("got %q; want %q", got, want)
		}
	})

	t.Run("LOCALAPPDATA when ORBIT_HOME empty", func(t *testing.T) {
		local := t.TempDir()
		t.Setenv("ORBIT_HOME", "")
		t.Setenv("LOCALAPPDATA", local)
		got, err := orbitCacheDir("dacpac")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(local, "orbit", "cache", "dacpac"); got != want {
			t.Errorf("got %q; want %q", got, want)
		}
	})

	t.Run("HOME/.orbit when both empty", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("ORBIT_HOME", "")
		t.Setenv("LOCALAPPDATA", "")
		t.Setenv("HOME", home)
		got, err := orbitCacheDir("dacpac")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".orbit", "cache", "dacpac"); got != want {
			t.Errorf("got %q; want %q", got, want)
		}
	})
}
