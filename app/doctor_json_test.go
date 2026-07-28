package app

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
)

func TestHumanDoctorHidesInactiveDaemonImplementation(t *testing.T) {
	label, visible := humanDoctorCheck(daemon.DoctorCheck{
		Name:    "Daemon",
		Status:  daemon.CheckInfo,
		Message: "not running",
	}, true)
	if visible || label != "" {
		t.Fatalf("inactive daemon check label=%q visible=%v", label, visible)
	}
}

func TestHumanDoctorCallsRuntimeHealthEnvironment(t *testing.T) {
	label, visible := humanDoctorCheck(daemon.DoctorCheck{
		Name:    "Daemon",
		Status:  daemon.CheckPass,
		Message: "All healthy (2/2)",
	}, true)
	if !visible || label != "Environment" {
		t.Fatalf("runtime health label=%q visible=%v", label, visible)
	}
}

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

func TestDoctorReadyEnvironmentPointsDirectlyToUp(t *testing.T) {
	resp := &daemon.DoctorResponse{
		Checks: []daemon.DoctorCheck{
			{Name: "Docker", Status: daemon.CheckPass, Message: "ok"},
			{Name: "Daemon", Status: daemon.CheckInfo, Message: "not running", Hint: "run: orbit up"},
		},
	}
	if !doctorReadyToStart(resp) {
		t.Fatal("ready stopped environment was not recognized")
	}
	actions := doctorRecommendedActions(resp)
	if len(actions) != 1 || actions[0].Command != "orbit up --json" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestDoctorFailureDoesNotRecommendStarting(t *testing.T) {
	resp := &daemon.DoctorResponse{
		Checks: []daemon.DoctorCheck{
			{Name: "Python", Status: daemon.CheckFail, Message: "not found"},
			{Name: "Daemon", Status: daemon.CheckInfo, Message: "not running", Hint: "run: orbit up"},
		},
	}
	if doctorReadyToStart(resp) {
		t.Fatal("failed prerequisites were treated as ready")
	}
	for _, action := range doctorRecommendedActions(resp) {
		if action.Command == "orbit up --json" {
			t.Fatalf("failed doctor recommended startup: %+v", action)
		}
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

func TestDoctorRecommendedActionsUseJSONForOrbitHints(t *testing.T) {
	resp := &daemon.DoctorResponse{
		Checks: []daemon.DoctorCheck{
			{Name: "Daemon", Status: daemon.CheckFail, Message: "api degraded", Hint: "run: orbit logs api"},
		},
	}
	got := doctorRecommendedActions(resp)
	if len(got) != 1 || got[0].Command != "orbit logs api --json" {
		t.Fatalf("actions = %+v", got)
	}
}

func TestLocalPortChecksDetectIPv4LoopbackOwner(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	portNumber := listener.Addr().(*net.TCPAddr).Port
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Ports: map[string]config.PortDef{"http": {Host: portNumber, Target: portNumber}}},
	}}

	checks := localPortChecks(cfg)
	if len(checks) != 1 || checks[0].Name != "Port "+strconv.Itoa(portNumber) ||
		checks[0].Status != daemon.CheckFail {
		t.Fatalf("checks = %+v", checks)
	}
	actions := doctorRecommendedActions(&daemon.DoctorResponse{Checks: checks})
	if len(actions) != 1 || actions[0].Command == "orbit up --json" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestDoctorResolvedPortConflictPointsDirectlyToResourceRetry(t *testing.T) {
	resp := &daemon.DoctorResponse{Checks: []daemon.DoctorCheck{
		{Name: "Port 28080", Status: daemon.CheckPass, Message: "available (api); previous conflict resolved"},
		{Name: "Daemon", Status: daemon.CheckPass, Message: "api ready to retry", Hint: "run: orbit up api"},
	}}
	if got := doctorStartCommand(resp); got != "orbit up api" {
		t.Fatalf("start command = %q", got)
	}
	actions := doctorRecommendedActions(resp)
	if len(actions) != 1 || actions[0].Command != "orbit up api --json" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestUpdateDoctorCheckOnlyRecommendsRestart(t *testing.T) {
	resp := addUpdateDoctorCheck(
		&daemon.DoctorResponse{Checks: []daemon.DoctorCheck{{
			Name:   "Docker",
			Status: daemon.CheckPass,
		}}},
		&daemon.VersionResponse{
			Running:         "v0.0.1",
			OnDisk:          "v0.0.2",
			UpdateAvailable: true,
		},
	)
	if len(resp.Checks) != 2 || resp.Checks[0].Name != "Orbit update" {
		t.Fatalf("checks = %+v", resp.Checks)
	}
	actions := doctorRecommendedActions(resp)
	if len(actions) != 1 || actions[0].Command != "orbit daemon restart --json" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestLocalDoctorResponseUnavailableSelectionOffersExactSwitch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	envs := filepath.Join(home, "envs")
	if err := os.MkdirAll(envs, 0755); err != nil {
		t.Fatal(err)
	}
	available := filepath.Join(envs, "renamed.yaml")
	if err := os.WriteFile(available, []byte("version: \"2\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(envs, "original.yaml")
	if err := writeCurrentEnv(missing); err != nil {
		t.Fatal(err)
	}
	previousConfig := configFile
	configFile = missing
	t.Cleanup(func() { configFile = previousConfig })

	resp := localDoctorResponse()
	if len(resp.Checks) == 0 || resp.Checks[0].Name != "Environment selection" {
		t.Fatalf("checks = %+v", resp.Checks)
	}
	if resp.Checks[0].Hint != "run: orbit switch renamed" {
		t.Fatalf("hint = %q", resp.Checks[0].Hint)
	}
	actions := doctorRecommendedActions(resp)
	if len(actions) != 1 || actions[0].Command != "orbit switch renamed --json" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestLocalDoctorResponseUnavailableSelectionKeepsRunningDaemonTruthful(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	envs := filepath.Join(home, "envs")
	if err := os.MkdirAll(envs, 0755); err != nil {
		t.Fatal(err)
	}
	available := filepath.Join(envs, "renamed.yaml")
	if err := os.WriteFile(available, []byte("version: \"2\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(envs, "original.yaml")
	if err := writeCurrentEnv(missing); err != nil {
		t.Fatal(err)
	}
	previousConfig := configFile
	configFile = missing
	t.Cleanup(func() { configFile = previousConfig })

	resp := localDoctorResponseWithDaemon(true)
	for _, check := range resp.Checks {
		if check.Name == "Daemon" {
			if check.Message != "running with the previous environment snapshot" {
				t.Fatalf("daemon check = %+v", check)
			}
			return
		}
	}
	t.Fatalf("daemon check missing: %+v", resp.Checks)
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
	if got := err.Error(); got != "doctor found 2 failed check(s): Environment, Docker" {
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
