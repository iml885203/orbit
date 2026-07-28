package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
)

func TestPreflightExplicitConfigDoesNotRequireSyncedEnvironments(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	dir := t.TempDir()
	envPath := filepath.Join(dir, "orbit.yaml")
	if err := os.WriteFile(envPath, []byte("version: \"2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previousConfig := configFile
	configFile = envPath
	t.Cleanup(func() { configFile = previousConfig })

	if err := preflightOrAbort(true, nil); err != nil {
		t.Fatalf("explicit config blocked by env repository readiness: %v", err)
	}
}

func TestPreflightSelectedEnvironmentStillRequiresInitialization(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	if err := preflightOrAbort(false, nil); err == nil {
		t.Fatal("missing env repository accepted without an explicit config")
	}
}

func TestPreflightBlocksUnsatisfiedPythonRequirementsWithSetupAction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	project := t.TempDir()
	requirements := filepath.Join(project, "requirements.txt")
	if err := os.WriteFile(requirements, []byte("humanize==4.12.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	interpreter := filepath.Join(t.TempDir(), "python3")
	if err := os.WriteFile(interpreter, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), "orbit.yaml")
	config := "version: \"2\"\nservices:\n  api:\n    type: python\n    path: " + project + "\n    command: " + interpreter + " app.py\n"
	if err := os.WriteFile(envPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	previousConfig := configFile
	configFile = envPath
	t.Cleanup(func() { configFile = previousConfig })

	err := preflightOrAbort(true, nil)
	if err == nil || !strings.Contains(err.Error(), "requirements.txt is not satisfied for api") {
		t.Fatalf("preflight error = %v", err)
	}
	withActions, ok := err.(interface{ CLIJSONReplacementActions() []cli.JSONAction })
	if !ok {
		t.Fatalf("error has no exact actions: %T", err)
	}
	actions := withActions.CLIJSONReplacementActions()
	if len(actions) != 1 || !strings.Contains(actions[0].Command, "-m pip install") {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestInfrastructurePreflightIgnoresHostProjectRequirements(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "orbit.yaml")
	config := "version: \"2\"\nservices:\n  api:\n    type: python\n    path: /missing/project\n    command: python3 app.py\n"
	if err := os.WriteFile(envPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	previousConfig, previousInfra := configFile, infraOnly
	configFile, infraOnly = envPath, true
	t.Cleanup(func() {
		configFile, infraOnly = previousConfig, previousInfra
	})

	if err := preflightOrAbort(true, nil); err != nil {
		t.Fatalf("infra-only blocked by host project: %v", err)
	}
}
