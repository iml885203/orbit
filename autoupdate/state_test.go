package autoupdate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStateIsSharedOutsideRuntimeHomes(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	launch := filepath.Join(t.TempDir(), "bin", "orbit")
	state, err := Load(launch)
	if err != nil {
		t.Fatal(err)
	}
	if state.Policy != PolicyAutomatic || state.Owner != OwnerDirect {
		t.Fatalf("unexpected defaults: %+v", state)
	}
	state.Policy = PolicyOff
	if err := Save(state); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORBIT_HOME", filepath.Join(t.TempDir(), "instances", "other"))
	got, err := Load(launch)
	if err != nil {
		t.Fatal(err)
	}
	if got.Policy != PolicyOff {
		t.Fatalf("policy = %q, want %q", got.Policy, PolicyOff)
	}
}

func TestInstallationIdentityDistinguishesLaunchers(t *testing.T) {
	first, _, err := InstallationID(filepath.Join(t.TempDir(), "one", "orbit"))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := InstallationID(filepath.Join(t.TempDir(), "two", "orbit"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("distinct launchers share installation ID")
	}
}

func TestOwnerFollowsHomebrewLauncherWithoutUsingVersionedTargetAsIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture")
	}
	root := t.TempDir()
	target := filepath.Join(root, "Cellar", "orbit", "0.16.0", "bin", "orbit")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(root, "bin", "orbit")
	if err := os.MkdirAll(filepath.Dir(launcher), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, launcher); err != nil {
		t.Fatal(err)
	}
	if got := Owner(launcher); got != OwnerHomebrew {
		t.Fatalf("owner = %q, want homebrew", got)
	}
}

func TestUpdateSerializesStateChanges(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	launch := filepath.Join(t.TempDir(), "orbit")
	_, err := Update(launch, func(state *State) error {
		state.DisclosureShown = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Load(launch)
	if err != nil {
		t.Fatal(err)
	}
	if !got.DisclosureShown {
		t.Fatal("update was not persisted")
	}
}

func TestLoadQuarantinesCorruptRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ORBIT_UPDATE_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "update-registry.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := Load(filepath.Join(t.TempDir(), "orbit"))
	if err != nil {
		t.Fatal(err)
	}
	if state.Policy != PolicyAutomatic {
		t.Fatalf("policy = %q", state.Policy)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "update-registry.json.corrupt-*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("quarantined registries = %v, err = %v", matches, err)
	}
}

func TestTransactionWritesRejectStaleWorker(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	launch := filepath.Join(t.TempDir(), "orbit")
	state, err := BeginTransaction(launch, "update", "v0.17.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FinishTransaction(launch, "different", "failed", nil); err == nil {
		t.Fatal("stale worker finished the active transaction")
	}
	if _, err := RecordRuntimeOutcome(launch, "different", "default", RuntimeOutcome{}); err == nil {
		t.Fatal("stale worker recorded an outcome")
	}
	if _, err := SetTransactionWorker(launch, "different", 123); err == nil {
		t.Fatal("stale launcher recorded a worker PID")
	}
	if _, err := FinishTransaction(launch, state.Transaction.ID, "failed", nil); err != nil {
		t.Fatal(err)
	}
}
