package app

import (
	"path/filepath"
	"testing"

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
}
