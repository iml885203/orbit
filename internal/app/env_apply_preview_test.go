package app

import (
	"bytes"
	"context"
	"github.com/iml885203/orbit/internal/autoupdate"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/spf13/cobra"
)

func TestEnvironmentApplyPreviewCommandPolicy(t *testing.T) {
	oldJSON := cli.JSONOutput
	defer func() { cli.JSONOutput = oldJSON }()
	cli.JSONOutput = true
	root := &cobra.Command{Use: "orbit"}
	env := &cobra.Command{Use: "env"}
	root.AddCommand(env)
	cmd := envApplyCmd()
	env.AddCommand(cmd)
	if !updateMutationCommand(cmd) || !shouldRecordCLI(cmd) || !lifecycleCommandHasBudget(cmd) {
		t.Fatal("real apply lost existing gates")
	}
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	if !environmentApplyPreviewCommand(cmd) || updateMutationCommand(cmd) || shouldRecordCLI(cmd) || lifecycleCommandHasBudget(cmd) || prepareLifecycleCommandContext(cmd) {
		t.Fatal("preview entered a mutating lifecycle policy")
	}
	if err := cmd.Flags().Set("dry-run", "false"); err != nil {
		t.Fatal(err)
	}
	if !updateMutationCommand(cmd) || !shouldRecordCLI(cmd) || !lifecycleCommandHasBudget(cmd) {
		t.Fatal("explicit false must keep real apply gates")
	}
}

func TestEnvironmentApplyPreviewNoDaemonKeepsStaleRuntimeFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	oldConfig := configFile
	defer func() { configFile = oldConfig }()
	configFile = filepath.Join(t.TempDir(), "orbit.yaml")
	if err := os.WriteFile(configFile, []byte("version: \"3\"\nservices: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// An invalid PID record must not trigger the normal stale-file cleanup.
	for path, contents := range map[string]string{daemon.DefaultPIDPath(): "999999999", daemon.DefaultSocketPath(): "preserve socket evidence", daemon.DefaultSettingsPath(): "{}"} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := map[string][]byte{}
	for _, path := range []string{daemon.DefaultPIDPath(), daemon.DefaultSocketPath(), daemon.DefaultSettingsPath()} {
		before[path], _ = os.ReadFile(path)
	}
	preview, err := previewEnvironmentChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if preview.DaemonRunning || len(preview.StopResources) != 0 {
		t.Fatalf("preview=%+v", preview)
	}
	for path, data := range before {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("preview mutated %s: %s %v", path, got, err)
		}
	}
	if err := os.WriteFile(configFile, []byte("version: \"3\"\nservices: invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := previewEnvironmentChanges(context.Background()); err == nil {
		t.Fatal("invalid config accepted without daemon")
	}
}

func TestPreviewContextDoesNotMigrateLegacySources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	t.Setenv("ORBIT_ENV_REPO_URL", "https://example.invalid/legacy.git")
	oldConfig, oldKind, oldGiven := configFile, configContextKind, configFlagGiven
	defer func() { configFile, configContextKind, configFlagGiven = oldConfig, oldKind, oldGiven }()
	configFile = filepath.Join(home, "legacy.yaml")
	if err := os.WriteFile(configFile, []byte("version: \"3\"\nservices: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	settings := []byte(`{"env_repo_url":"https://example.invalid/legacy.git","workspace_root":"legacy"}`)
	if err := os.WriteFile(daemon.DefaultSettingsPath(), settings, 0600); err != nil {
		t.Fatal(err)
	}
	root := &cobra.Command{Use: "orbit"}
	root.PersistentFlags().String("config", "", "")
	env := &cobra.Command{Use: "env"}
	cmd := envApplyCmd()
	root.AddCommand(env)
	env.AddCommand(cmd)
	_ = cmd.Flags().Set("dry-run", "true")
	if err := prepareRootEnvironment(cmd); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(daemon.DefaultSettingsPath())
	if !bytes.Equal(after, settings) {
		t.Fatal("preview migrated legacy settings")
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("preview created migration state: %v", entries)
	}
}

func TestEnvironmentApplyPreviewBypassesPendingAutomaticUpdate(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	launch := filepath.Join(t.TempDir(), "orbit")
	os.Args = []string{launch}
	state, err := autoupdate.BeginTransaction(launch, "update", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	root := &cobra.Command{Use: "orbit"}
	env := &cobra.Command{Use: "env"}
	cmd := envApplyCmd()
	root.AddCommand(env)
	env.AddCommand(cmd)
	_ = cmd.Flags().Set("dry-run", "true")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)
	if err := waitForPendingAutomaticUpdate(cmd); err != nil {
		t.Fatalf("preview entered pending-update gate: %v", err)
	}
	after, err := autoupdate.Load(launch)
	if err != nil {
		t.Fatal(err)
	}
	if after.Transaction == nil || after.Transaction.ID != state.Transaction.ID || after.Transaction.FinishedAt != nil {
		t.Fatal("preview recovered or changed pending transaction")
	}
	_ = cmd.Flags().Set("dry-run", "false")
	start := time.Now()
	if err := waitForPendingAutomaticUpdate(cmd); err == nil {
		t.Fatal("real apply lost canceled mutation gate")
	}
	if time.Since(start) > time.Second {
		t.Fatal("canceled real apply blocked")
	}
}
