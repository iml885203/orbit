package instance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/atomicio"
)

// Targeting an instance must not bring it into being. `--instance <name>` is
// applied in PersistentPreRun, so every command carrying it — including
// read-only ones like `settings list` and the `inspect` the agent contract
// recommends for diagnosis — used to leave a home directory behind. The
// residue is invisible to `orbit instance list`, which filters homes without
// a manifest, so a typo'd name accumulated silently.
func TestActivateDoesNotCreateTheInstanceHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv(EnvBaseHome, base)

	runtime, err := Activate("read-only-probe")
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if _, err := os.Stat(runtime.Home); !os.IsNotExist(err) {
		t.Errorf("Activate created %s (err=%v); resolving a target must not write", runtime.Home, err)
	}
	// The environment still has to be set — that is what activation is for.
	if got := os.Getenv("ORBIT_HOME"); got != runtime.Home {
		t.Errorf("ORBIT_HOME = %q, want %q", got, runtime.Home)
	}
	if got := os.Getenv("ORBIT_NAMESPACE"); got != runtime.Namespace {
		t.Errorf("ORBIT_NAMESPACE = %q, want %q", got, runtime.Namespace)
	}
}

// A writer must still work with no home on disk: the directory is created at
// the point of writing, not ahead of it.
func TestWritingUnderAnUncreatedInstanceHomeSucceeds(t *testing.T) {
	base := t.TempDir()
	t.Setenv(EnvBaseHome, base)

	runtime, err := Activate("writer-probe")
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if _, err := os.Stat(runtime.Home); !os.IsNotExist(err) {
		t.Fatalf("home exists before the write; test would not prove anything")
	}

	target := filepath.Join(runtime.Home, "settings.json")
	if err := atomicio.WriteFile(target, []byte(`{"env_toggles":{}}`), 0o644); err != nil {
		t.Fatalf("write under an uncreated home: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("file missing after write: %v", err)
	}
}
