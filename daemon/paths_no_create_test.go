package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// Asking for a path is not a reason to write to disk. OrbitDir used to
// MkdirAll its result, so a pure lookup — DefaultSettingsPath, which every
// read command reaches — materialised the directory. Under an instance
// target that meant reading about an instance created it.
func TestPathLookupsDoNotCreateTheHome(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "instances", "never-created")
	t.Setenv("ORBIT_HOME", home)

	for name, lookup := range map[string]func() string{
		"OrbitDir":            OrbitDir,
		"DefaultSettingsPath": DefaultSettingsPath,
		"DefaultSocketPath":   DefaultSocketPath,
	} {
		t.Run(name, func(t *testing.T) {
			_ = lookup()
			if _, err := os.Stat(home); !os.IsNotExist(err) {
				t.Errorf("%s created %s (err=%v)", name, home, err)
			}
		})
	}
}

// Every other test here sets ORBIT_HOME, which returns early — so the branch
// real users take, deriving ~/.orbit from the home directory, would go
// unexercised. Restoring the mkdir there reintroduces the whole defect for
// anyone not running with an override.
func TestDefaultHomeLookupDoesNotCreateIt(t *testing.T) {
	fake := t.TempDir()
	t.Setenv("ORBIT_HOME", "")
	t.Setenv("HOME", fake)
	t.Setenv("USERPROFILE", fake)
	t.Setenv("LOCALAPPDATA", "")

	dir := OrbitDir()
	if dir == "" {
		t.Fatal("OrbitDir returned empty")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("OrbitDir created %s (err=%v) without an ORBIT_HOME override", dir, err)
	}
}

// Reading settings from a home that does not exist is the ordinary first-run
// case and must stay silent — the distinction added for unreadable settings
// depends on a missing file not being reported as a failure.
func TestReadingSettingsUnderAMissingHomeIsNotAnError(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "instances", "never-created")
	t.Setenv("ORBIT_HOME", home)

	s := LoadSettings(DefaultSettingsPath())
	if err := s.LoadError(); err != nil {
		t.Errorf("LoadError = %v, want nil for a home that does not exist", err)
	}
}

// Removing OrbitDir's mkdir broke daemon startup: the log is opened with
// os.OpenFile, which does not create the parent, so a first run under a home
// that did not exist failed with "opening daemon log: no such file or
// directory". Writers now say so explicitly.
func TestEnsureOrbitDirCreatesTheHomeForWriters(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "instances", "writer")
	t.Setenv("ORBIT_HOME", home)

	got, err := EnsureOrbitDir()
	if err != nil {
		t.Fatalf("EnsureOrbitDir: %v", err)
	}
	if got != home {
		t.Errorf("returned %q, want %q", got, home)
	}
	if _, err := os.Stat(home); err != nil {
		t.Errorf("home not created: %v", err)
	}

}

// The production writers must each ensure the home themselves. Testing
// EnsureOrbitDir alone would pass even if no caller used it, which is how the
// daemon-log and socket regressions were introduced in the first place.
func TestWritersCreateTheHomeThemselves(t *testing.T) {
	t.Run("daemon log", func(t *testing.T) {
		base := t.TempDir()
		home := filepath.Join(base, "instances", "log-writer")
		t.Setenv("ORBIT_HOME", home)

		f, err := OpenDaemonLog()
		if err != nil {
			t.Fatalf("OpenDaemonLog under a home that does not exist: %v", err)
		}
		_ = f.Close()
		if _, err := os.Stat(DefaultLogPath()); err != nil {
			t.Errorf("log missing after open: %v", err)
		}
	})

	t.Run("unix socket", func(t *testing.T) {
		// A short base: the socket path has an OS-imposed byte budget that
		// t.TempDir's name would blow on its own.
		base, err := os.MkdirTemp("/tmp", "ob")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(base) })
		home := filepath.Join(base, "i")
		t.Setenv("ORBIT_HOME", home)

		// A bind does not create its parent, so the server has to ensure the
		// home before listening. Without that, a first run crashes.
		if _, err := EnsureOrbitDir(); err != nil {
			t.Fatalf("EnsureOrbitDir: %v", err)
		}
		ln, err := net.Listen("unix", DefaultSocketPath())
		if err != nil {
			t.Fatalf("bind after ensuring the home: %v", err)
		}
		_ = ln.Close()
	})
}
