package autoupdate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestVerificationEvidenceSurvivesRegistryReload(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	launch := filepath.Join(t.TempDir(), "orbit")
	want := &VerificationRecord{
		PolicyVersion: "github-release-v1", Repository: "iml885203/orbit", Tag: "v0.17.0",
		TargetCommit: strings.Repeat("a", 40), AssetName: "orbit-linux-amd64",
		AssetSHA256: strings.Repeat("b", 64), ChecksumsSHA256: strings.Repeat("c", 64),
		VerifiedAt: time.Now().UTC().Truncate(time.Second),
	}
	if _, err := Update(launch, func(state *State) error {
		state.StagedBinary = filepath.Join(t.TempDir(), "orbit")
		state.StagedEvidence = want
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(launch)
	if err != nil {
		t.Fatal(err)
	}
	if got.StagedEvidence == nil || *got.StagedEvidence != *want {
		t.Fatalf("evidence=%+v want=%+v", got.StagedEvidence, want)
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

func TestFinishTransactionEvidenceLifecycle(t *testing.T) {
	for _, phase := range []string{"succeeded", "partial", "failed"} {
		t.Run(phase, func(t *testing.T) {
			t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
			launch := filepath.Join(t.TempDir(), "orbit")
			evidence := &VerificationRecord{PolicyVersion: githubReleasePolicyVersion}
			state, err := Update(launch, func(next *State) error {
				next.TargetVersion, next.StagedBinary, next.StagedEvidence = "v2", "staged", evidence
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			state, err = BeginTransaction(launch, "update", "v2")
			if err != nil {
				t.Fatal(err)
			}
			state, err = FinishTransaction(launch, state.Transaction.ID, phase, nil)
			if err != nil {
				t.Fatal(err)
			}
			cleared := state.StagedEvidence == nil && state.StagedBinary == ""
			if cleared != (phase != "failed") {
				t.Fatalf("phase=%s cleared=%v state=%+v", phase, cleared, state)
			}
		})
	}
}

func TestSummaryNamesTheUpgradeCommandOfThePackageManagerThatOwnsTheInstall(t *testing.T) {
	for owner, want := range map[string]string{
		OwnerHomebrew: "brew upgrade iml885203/tap/orbit",
		OwnerScoop:    "scoop update orbit",
		OwnerDirect:   "",
	} {
		if got := (State{Owner: owner}).Summary().UpgradeCommand; got != want {
			t.Errorf("owner %q: upgrade command = %q, want %q", owner, got, want)
		}
	}
}

func TestReconcileInstalledSettlesTargetThePackageManagerAlreadyInstalled(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	launch := filepath.Join(t.TempDir(), "bin", "orbit")
	state, err := Load(launch)
	if err != nil {
		t.Fatal(err)
	}
	state.Owner = OwnerHomebrew
	state.Policy = PolicyOff
	state.CurrentVersion = "v0.16.5 (2026-10-14 17:10:53 +0800)"
	state.TargetVersion = "v0.16.6"
	state.Phase = "available"
	if err := Save(state); err != nil {
		t.Fatal(err)
	}

	got, err := ReconcileInstalled(launch, "v0.16.6 (2026-10-18 18:32:43 +0800)")
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetVersion != "" || got.Phase != "current" || got.CurrentVersion != "v0.16.6 (2026-10-18 18:32:43 +0800)" {
		t.Fatalf("reconciled state = %+v", got)
	}
	persisted, err := Load(launch)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.TargetVersion != "" || persisted.Phase != "current" {
		t.Fatalf("persisted state = %+v", persisted)
	}
}

func TestReconcileInstalledClearsStagedBinaryAHandRunInstallerSuperseded(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	launch := filepath.Join(t.TempDir(), "bin", "orbit")
	state, err := Load(launch)
	if err != nil {
		t.Fatal(err)
	}
	state.TargetVersion = "v0.16.6"
	state.Phase = "ready"
	state.StagedBinary = "/staged/orbit"
	state.ApplyEligible = true
	if err := Save(state); err != nil {
		t.Fatal(err)
	}

	got, err := ReconcileInstalled(launch, "v0.17.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetVersion != "" || got.StagedBinary != "" || got.ApplyEligible || got.Phase != "current" {
		t.Fatalf("reconciled state = %+v", got)
	}
}

func TestReconcileInstalledKeepsWhatItCannotSettle(t *testing.T) {
	now := time.Now()
	for name, tc := range map[string]struct {
		installed, target, phase string
		transaction              *Transaction
	}{
		"older installed release": {installed: "v0.16.5 (2026-10-14)", target: "v0.16.6", phase: "ready"},
		"unordered source build":  {installed: "82d6473a1b2c (2026-10-26)", target: "v0.16.6", phase: "ready"},
		"unknown build":           {installed: "unknown", target: "v0.16.6", phase: "available"},
		"empty build":             {installed: "", target: "v0.16.6", phase: "available"},
		"apply in flight": {installed: "v0.16.6", target: "v0.16.6", phase: "applying",
			transaction: &Transaction{ID: "t", Operation: "update", Phase: "applying", StartedAt: now}},
		"failed rollback to an older build": {installed: "v0.16.6", target: "v0.16.5", phase: "failed",
			transaction: &Transaction{ID: "t", Operation: "rollback", Phase: "failed", TargetVersion: "v0.16.5", Error: "kept", StartedAt: now, FinishedAt: &now}},
		"rollback whose target could not be probed": {installed: "v0.16.6", target: "previous", phase: "failed",
			transaction: &Transaction{ID: "t", Operation: "rollback", Phase: "failed", TargetVersion: "previous", Error: "kept", StartedAt: now, FinishedAt: &now}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
			launch := filepath.Join(t.TempDir(), "bin", "orbit")
			state, err := Load(launch)
			if err != nil {
				t.Fatal(err)
			}
			state.TargetVersion = tc.target
			state.Phase = tc.phase
			state.LastError = "kept"
			state.Transaction = tc.transaction
			if err := Save(state); err != nil {
				t.Fatal(err)
			}

			got, err := ReconcileInstalled(launch, tc.installed)
			if err != nil {
				t.Fatal(err)
			}
			if got.TargetVersion != tc.target || got.Phase != tc.phase || got.LastError != "kept" {
				t.Fatalf("state changed: %+v", got)
			}
		})
	}
}

func TestReconcileInstalledSettlesAFailedCheckEvenAfterAnEarlierFailedApply(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	launch := filepath.Join(t.TempDir(), "bin", "orbit")
	state, err := Load(launch)
	if err != nil {
		t.Fatal(err)
	}
	finished := time.Now()
	state.TargetVersion = "v0.16.6"
	state.Phase = "failed"
	state.LastError = "check release: offline"
	state.Transaction = &Transaction{ID: "t", Operation: "update", Phase: "failed", Error: "verify replaced binary", StartedAt: finished, FinishedAt: &finished}
	if err := Save(state); err != nil {
		t.Fatal(err)
	}
	if state.RecordsTransactionOutcome() {
		t.Fatal("a check failure must not read as the earlier apply's outcome")
	}

	got, err := ReconcileInstalled(launch, "v0.16.6")
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetVersion != "" || got.Phase != "current" || got.LastError != "" {
		t.Fatalf("reconciled state = %+v", got)
	}
}
