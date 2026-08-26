package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iml885203/orbit/autoupdate"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/instance"
	"github.com/spf13/cobra"
)

func TestUpdateMutationCommandKeepsObservationAvailable(t *testing.T) {
	root := &cobra.Command{Use: "orbit"}
	status := &cobra.Command{Use: "status"}
	up := &cobra.Command{Use: "up"}
	root.AddCommand(status, up)
	if updateMutationCommand(status) {
		t.Fatal("status must observe a pending update without waiting")
	}
	if !updateMutationCommand(up) {
		t.Fatal("up must wait for a pending update transaction")
	}
}

func TestWorkerTransactionBypassesItsOwnPendingGate(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	t.Setenv("ORBIT_HOME", t.TempDir())
	launch := filepath.Join(t.TempDir(), "orbit")
	t.Setenv(autoupdate.EnvLaunchPath, launch)
	state, err := autoupdate.BeginTransaction(launch, "update", "v0.17.0")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(daemon.UpdateTransactionEnv, state.Transaction.ID)
	root := &cobra.Command{Use: "orbit"}
	up := &cobra.Command{Use: "up"}
	root.AddCommand(up)
	if err := waitForPendingAutomaticUpdate(up); err != nil {
		t.Fatal(err)
	}
}

func TestPackageManagedUpdateHasOneOwnerAction(t *testing.T) {
	update := &autoupdate.Summary{Owner: autoupdate.OwnerHomebrew, Phase: "available", TargetVersion: "v0.17.0"}
	if !updateNeedsUserAction(update) {
		t.Fatal("package-managed release must require its owner action")
	}
	if command := releaseUpdateCommand(update, true); command != "brew upgrade orbit" {
		t.Fatalf("command = %q", command)
	}
}

func TestPackageManagerDetectionFollowsHomebrewSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture")
	}
	root := t.TempDir()
	target := filepath.Join(root, "Cellar", "orbit", "0.17.0", "bin", "orbit")
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
	managed := packageManagerForBinary(launcher, runtime.GOOS)
	if managed == nil || managed.command != "brew upgrade orbit" {
		t.Fatalf("managed = %+v", managed)
	}
}

func TestAutomaticUpdateSettingUsesDocumentedKey(t *testing.T) {
	if got, err := translateSettingsKey("automatic-updates"); err != nil || got != "automatic_updates" {
		t.Fatalf("translated = %q, err = %v", got, err)
	}
}

func TestNamedContextStillDiscoversDefaultRuntimeSocket(t *testing.T) {
	base := t.TempDir()
	t.Setenv(instance.EnvBaseHome, base)
	t.Setenv(instance.EnvName, "agent-one")
	t.Setenv("ORBIT_HOME", filepath.Join(base, "instances", "agent-one"))
	sockets := discoverableDefaultRuntimeSockets()
	wantDefault := filepath.Join(base, "orbit.sock")
	wantNamed := filepath.Join(base, "instances", "agent-one", "orbit.sock")
	if len(sockets) != 2 || sockets[0] != wantDefault || sockets[1] != wantNamed {
		t.Fatalf("sockets = %v, want [%s %s]", sockets, wantDefault, wantNamed)
	}
}

func TestUpdateWorkerReplacesVerifiedBinaryAndKeepsBackup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture is a POSIX executable script")
	}
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	t.Setenv("ORBIT_HOME", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(dir, "orbit")
	staged := filepath.Join(dir, "staged")
	oldScript := "#!/bin/sh\necho 'v0.16.0'\n"
	newScript := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'v0.17.0'; else echo '{\"schema_version\":\"orbit.cli.v1\",\"ok\":true}'; fi\n"
	if err := os.WriteFile(target, []byte(oldScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte(newScript), 0o755); err != nil {
		t.Fatal(err)
	}
	state, err := autoupdate.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	state.TargetVersion = "v0.17.0"
	state.StagedBinary = staged
	if err := autoupdate.Save(state); err != nil {
		t.Fatal(err)
	}
	state, err = autoupdate.BeginTransaction(target, "update", state.TargetVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := runUpdateWorker("update", target, staged, state.InstallationID, state.Transaction.ID, 0); err != nil {
		t.Fatal(err)
	}
	installed, _ := os.ReadFile(target)
	backup, _ := os.ReadFile(target + ".prev")
	if !strings.Contains(string(installed), "orbit.cli.v1") || string(backup) != oldScript {
		t.Fatalf("installed=%q backup=%q", installed, backup)
	}
	final, err := autoupdate.Load(target)
	if err != nil || final.Phase != "succeeded" {
		t.Fatalf("final=%+v err=%v", final, err)
	}
}

func TestUpdateWorkerRollsBackWrongReportedVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture is a POSIX executable script")
	}
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	t.Setenv("ORBIT_HOME", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(dir, "orbit")
	staged := filepath.Join(dir, "staged")
	oldScript := "#!/bin/sh\necho 'v0.16.0'\n"
	wrongScript := "#!/bin/sh\necho 'v9.9.9'\n"
	if err := os.WriteFile(target, []byte(oldScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte(wrongScript), 0o755); err != nil {
		t.Fatal(err)
	}
	state, err := autoupdate.Update(target, func(next *autoupdate.State) error {
		next.TargetVersion = "v0.17.0"
		next.StagedBinary = staged
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err = autoupdate.BeginTransaction(target, "update", state.TargetVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := runUpdateWorker("update", target, staged, state.InstallationID, state.Transaction.ID, 0); err == nil {
		t.Fatal("wrong target version was accepted")
	}
	installed, err := os.ReadFile(target)
	if err != nil || string(installed) != oldScript {
		t.Fatalf("installed=%q err=%v", installed, err)
	}
}
