package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
)

func TestDoctorRecommendedActionsAllPass(t *testing.T) {
	resp := &daemon.DoctorResponse{
		Checks: []daemon.DoctorCheck{
			{Name: "Docker", Status: daemon.CheckPass, Message: "ok"},
			{Name: "Config", Status: daemon.CheckInfo, Message: "path"},
		},
	}
	got := doctorRecommendedActions(resp)
	if len(got) != 1 {
		t.Fatalf("actions count = %d, want 1", len(got))
	}
	if got[0].Command != "orbit status --json" {
		t.Fatalf("command = %q", got[0].Command)
	}
}

func TestDoctorRecommendedActionsRunnableHint(t *testing.T) {
	resp := &daemon.DoctorResponse{
		Checks: []daemon.DoctorCheck{
			{Name: "SQL Registry Auth", Status: daemon.CheckFail, Message: "auth failed", Hint: "run: docker login registry.example"},
		},
	}
	got := doctorRecommendedActions(resp)
	found := false
	for _, action := range got {
		if action.Command == "docker login registry.example" {
			found = true
			if action.Destructive {
				t.Fatal("docker login action marked destructive")
			}
		}
	}
	if !found {
		t.Fatalf("missing docker login action: %+v", got)
	}
}

func TestDoctorFailureIgnoresHiddenServiceCheck(t *testing.T) {
	resp := &daemon.DoctorResponse{
		Checks: []daemon.DoctorCheck{
			{Name: "Daemon", Status: daemon.CheckFail, Message: "api degraded"},
			{Name: "Config", Status: daemon.CheckPass, Message: "ok"},
		},
	}

	if err := doctorFailure(resp, false); err != nil {
		t.Fatalf("hidden service check failed init doctor: %v", err)
	}
	if err := doctorFailure(resp, true); err == nil {
		t.Fatal("visible failed service check returned nil")
	}
}

func TestDoctorFailureIncludesAllFailedCheckNames(t *testing.T) {
	resp := &daemon.DoctorResponse{
		Checks: []daemon.DoctorCheck{
			{Name: "Daemon", Status: daemon.CheckFail},
			{Name: "Docker", Status: daemon.CheckFail},
			{Name: "Node.js", Status: daemon.CheckWarn},
		},
	}

	err := doctorFailure(resp, true)
	if err == nil {
		t.Fatal("doctorFailure returned nil")
	}
	if got := err.Error(); got != "doctor found 2 failed check(s): Daemon, Docker" {
		t.Fatalf("error = %q", got)
	}
}

// Regression: the daemon-down doctor fallback (used by doctor --json)
// consults the injected extensions' CLIDoctor hooks with the loaded
// config — and skips them entirely when the config failed to load. The
// feature-specific gate content (the ExampleTeam DB-workflow skip) is the
// extension's own concern, tested in its package.
func TestLocalDoctorResponse_ExtensionChecks(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.yaml")
	if err := os.WriteFile(envPath, []byte("version: \"2\"\ncontainers:\n  redis:\n    image: redis:7.4\n    ports:\n      redis: 6399\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := configFile
	prevExts := extensions
	configFile = envPath
	extensions = []extension.Extension{{
		Name: "test",
		CLIDoctor: &extension.CLIDoctor{
			Checks: func(cfg *config.Config) []daemon.DoctorCheck {
				if cfg == nil {
					t.Error("CLIDoctor.Checks invoked with nil cfg")
				}
				return []daemon.DoctorCheck{{Name: "Ext Check", Status: daemon.CheckInfo, Message: "hello"}}
			},
		},
	}}
	t.Cleanup(func() { configFile = prev; extensions = prevExts })

	resp := localDoctorResponse()
	var sawExt bool
	for _, c := range resp.Checks {
		if c.Name == "Ext Check" && c.Status == daemon.CheckInfo {
			sawExt = true
		}
	}
	if !sawExt {
		t.Error("extension CLIDoctor checks missing from the offline doctor response")
	}

	// A config that fails to load must not reach the extension hooks.
	configFile = filepath.Join(dir, "missing.yaml")
	extensions = []extension.Extension{{
		Name: "test",
		CLIDoctor: &extension.CLIDoctor{
			Checks: func(cfg *config.Config) []daemon.DoctorCheck {
				t.Error("CLIDoctor.Checks invoked despite config load failure")
				return nil
			},
		},
	}}
	_ = localDoctorResponse()
}

func TestLocalDoctorResponse_HostOnlyEnvironmentDoesNotRequireDocker(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.yaml")
	if err := os.WriteFile(envPath, []byte(`
version: "2"
services:
  docs:
    type: shell
    path: .
    command: echo ready
`), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := configFile
	configFile = envPath
	t.Cleanup(func() { configFile = prev })

	resp := localDoctorResponse()
	for _, check := range resp.Checks {
		if check.Name == "Docker" {
			t.Fatalf("host-only environment reported an irrelevant Docker check: %+v", check)
		}
	}
}
