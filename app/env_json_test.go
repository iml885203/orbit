package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/envsync"
)

func TestBuildEnvUseJSONData(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "development.yaml")
	got := buildEnvUseJSONData(envPath, true)

	if got.Operation != "env_use" {
		t.Fatalf("operation = %q", got.Operation)
	}
	if got.SelectedEnv != envPath {
		t.Fatalf("selected_env = %q", got.SelectedEnv)
	}
	if got.EnvName != "development.yaml" {
		t.Fatalf("env_name = %q", got.EnvName)
	}
	if !got.DaemonRunning {
		t.Fatal("daemon_running = false, want true")
	}
	if !got.RestartRequired {
		t.Fatal("restart_required = false, want true")
	}
}

func TestBuildEnvSyncJSONData(t *testing.T) {
	got := buildEnvSyncJSONData(envSyncJSONOptions{
		Source:        "file:///tmp/envs",
		Destination:   "/tmp/orbit/envs",
		DryRun:        true,
		Result:        envsync.Result{Written: []string{"a.yaml", "b.yaml"}},
		DaemonRunning: true,
		RestartAction: "recommended",
	})

	if got.Operation != "env_sync" {
		t.Fatalf("operation = %q", got.Operation)
	}
	if got.Source != "file:///tmp/envs" {
		t.Fatalf("source = %q", got.Source)
	}
	if got.Destination != "/tmp/orbit/envs" {
		t.Fatalf("destination = %q", got.Destination)
	}
	if !got.DryRun {
		t.Fatal("dry_run = false, want true")
	}
	if len(got.Written) != 2 {
		t.Fatalf("written = %+v", got.Written)
	}
	if !got.DaemonRunning {
		t.Fatal("daemon_running = false, want true")
	}
	if got.RestartAction != "recommended" {
		t.Fatalf("restart_action = %q", got.RestartAction)
	}
}

func TestEnvSyncRestartActionIsPure(t *testing.T) {
	tests := []struct {
		name          string
		filesChanged  bool
		daemonRunning bool
		dryRun        bool
		noRestart     bool
		want          string
	}{
		{"changed daemon running", true, true, false, false, "recommended"},
		{"dry run", true, true, true, false, "none"},
		{"no restart", true, true, false, true, "none"},
		{"no changes", false, true, false, false, "none"},
		{"daemon stopped", true, false, false, false, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := envSyncRestartAction(tt.filesChanged, tt.daemonRunning, tt.dryRun, tt.noRestart)
			if got != tt.want {
				t.Fatalf("envSyncRestartAction() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildSwitchJSONData(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "development.yaml")
	got := buildSwitchJSONData(switchJSONOptions{
		SelectedEnv:         envPath,
		DaemonAction:        "restart",
		DaemonRunningBefore: true,
		DaemonRunningAfter:  true,
		PID:                 123,
		ConfigPath:          envPath,
		Prerequisites: []daemon.DoctorCheck{{
			Name:    "Packages (web)",
			Status:  daemon.CheckFail,
			Message: "project packages are not installed",
			Hint:    "run: pnpm --dir /workspace/web install",
		}},
		PrerequisitesReady: false,
	})

	if got.Operation != "switch" {
		t.Fatalf("operation = %q", got.Operation)
	}
	if got.SelectedEnv != envPath {
		t.Fatalf("selected_env = %q", got.SelectedEnv)
	}
	if got.EnvName != "development.yaml" {
		t.Fatalf("env_name = %q", got.EnvName)
	}
	if got.DaemonAction != "restart" {
		t.Fatalf("daemon_action = %q", got.DaemonAction)
	}
	if !got.DaemonRunningBefore || !got.DaemonRunningAfter {
		t.Fatalf("daemon running before/after = %v/%v", got.DaemonRunningBefore, got.DaemonRunningAfter)
	}
	if got.PID != 123 {
		t.Fatalf("pid = %d", got.PID)
	}
	if got.PrerequisitesReady {
		t.Fatal("prerequisites_ready = true, want false")
	}
	if len(got.Prerequisites) != 1 || got.Prerequisites[0].Name != "Packages (web)" {
		t.Fatalf("prerequisites = %+v", got.Prerequisites)
	}
}

func TestSwitchRecommendedActionsIncludeExactPackageInstall(t *testing.T) {
	checks := []daemon.DoctorCheck{{
		Name:    "Packages (web)",
		Status:  daemon.CheckFail,
		Message: "project packages are not installed",
		Hint:    "run: pnpm --dir /workspace/web install",
	}}
	actions := switchRecommendedActions(checks)
	for _, action := range actions {
		if action.Command == "pnpm --dir /workspace/web install" {
			return
		}
	}
	t.Fatalf("actions = %+v", actions)
}

func TestSwitchPrerequisitesUseSelectedEnvironmentPackages(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), "node.yaml")
	raw := fmt.Sprintf(`
version: "2"
services:
  web:
    type: node
    path: %q
    command: pnpm dev
`, project)
	if err := os.WriteFile(envPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	checks, ready, err := switchPrerequisites(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("prerequisites ready despite missing runtime and packages")
	}
	for _, check := range checks {
		if check.Name == "Packages (web)" && check.Status == daemon.CheckFail {
			return
		}
	}
	t.Fatalf("checks = %+v", checks)
}
