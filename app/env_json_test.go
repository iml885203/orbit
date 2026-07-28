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
	if got.EnvName != "development" {
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
		ApplyAction:   "recommended",
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
	if got.ApplyAction != "recommended" {
		t.Fatalf("apply_action = %q", got.ApplyAction)
	}
}

func TestEnvSyncApplyActionIsPure(t *testing.T) {
	tests := []struct {
		name           string
		changesPending bool
		daemonRunning  bool
		dryRun         bool
		noApply        bool
		applied        bool
		want           string
	}{
		{"pending daemon running", true, true, false, false, false, "recommended"},
		{"applied", true, true, false, false, true, "applied"},
		{"dry run", true, true, true, false, false, "none"},
		{"no apply", true, true, false, true, false, "deferred"},
		{"already current", false, true, false, false, false, "none"},
		{"daemon stopped", true, false, false, false, false, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := envSyncApplyAction(
				tt.changesPending,
				tt.daemonRunning,
				tt.dryRun,
				tt.noApply,
				tt.applied,
			)
			if got != tt.want {
				t.Fatalf("envSyncApplyAction() = %q, want %q", got, tt.want)
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
	if got.EnvName != "development" {
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

func TestSwitchRecommendedActionsStartReadyEnvironment(t *testing.T) {
	actions := switchRecommendedActions(nil, true)
	if len(actions) != 1 || actions[0].Command != "orbit up --json" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestSwitchRecommendedActionsIncludeExactPackageInstall(t *testing.T) {
	checks := []daemon.DoctorCheck{{
		Name:    "Packages (web)",
		Status:  daemon.CheckFail,
		Message: "project packages are not installed",
		Hint:    "run: pnpm --dir /workspace/web install",
	}}
	actions := switchRecommendedActions(checks, false)
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
