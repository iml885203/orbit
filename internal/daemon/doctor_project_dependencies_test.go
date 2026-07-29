package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestPythonRequirementsFailBeforeStartupWithExactSetup(t *testing.T) {
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
	service := &config.Service{
		Type:    "python",
		Path:    project,
		Command: interpreter + " app.py",
	}

	check, supported := pythonProjectDependencyCheck("api", service)
	if !supported || check.Status != CheckFail {
		t.Fatalf("check = %+v, supported = %v", check, supported)
	}
	if !strings.Contains(check.Message, "requirements.txt is not satisfied for api") {
		t.Fatalf("message = %q", check.Message)
	}
	wantCommand := interpreter + " -m pip install --user -r " + requirements
	if check.Hint != "run: "+wantCommand {
		t.Fatalf("hint = %q, want %q", check.Hint, "run: "+wantCommand)
	}
	cfg := &config.Config{Services: map[string]*config.Service{"api": service}}
	if command, ok := ProjectDependencySetupCommand(cfg, "api"); !ok || command != wantCommand {
		t.Fatalf("setup = %q, %v", command, ok)
	}
}

func TestPythonRequirementsPassWhenExactInterpreterReportsSatisfied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "requirements.txt"), []byte("humanize==4.12.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	interpreter := filepath.Join(t.TempDir(), "python3")
	if err := os.WriteFile(interpreter, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	service := &config.Service{Type: "python", Path: project, Command: interpreter + " app.py"}

	check, supported := pythonProjectDependencyCheck("api", service)
	if !supported || check.Status != CheckPass {
		t.Fatalf("check = %+v, supported = %v", check, supported)
	}
	cfg := &config.Config{Services: map[string]*config.Service{"api": service}}
	if command, ok := ProjectDependencySetupCommand(cfg, "api"); ok || command != "" {
		t.Fatalf("unexpected setup = %q, %v", command, ok)
	}
}

func TestPythonRequirementsCoalesceSharedProjectSetup(t *testing.T) {
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
	cfg := &config.Config{Services: map[string]*config.Service{
		"api":    {Type: "python", Path: project, Command: interpreter + " api.py"},
		"worker": {Type: "python", Path: project, Command: interpreter + " worker.py"},
	}}

	checks := projectDependencyChecks(cfg)

	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want one shared setup", checks)
	}
	check := checks[0]
	if check.Name != "Packages (api, worker)" {
		t.Fatalf("name = %q", check.Name)
	}
	if check.Message != "requirements.txt is not satisfied for api, worker" {
		t.Fatalf("message = %q", check.Message)
	}
	wantHint := "run: " + interpreter + " -m pip install --user -r " + requirements
	if check.Hint != wantHint {
		t.Fatalf("hint = %q, want %q", check.Hint, wantHint)
	}
}

func TestPythonRequirementsSetupResolvesProjectRelativeInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	project := t.TempDir()
	requirements := filepath.Join(project, "requirements.txt")
	if err := os.WriteFile(requirements, []byte("humanize==4.12.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	venv := filepath.Join(project, ".venv", "bin")
	if err := os.MkdirAll(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	interpreter := filepath.Join(venv, "python")
	if err := os.WriteFile(interpreter, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	service := &config.Service{
		Type:    "python",
		Path:    project,
		Command: ".venv/bin/python app.py",
	}

	check, supported := pythonProjectDependencyCheck("api", service)
	if !supported || check.Status != CheckFail {
		t.Fatalf("check = %+v, supported = %v", check, supported)
	}
	want := "run: " + interpreter + " -m pip install --user -r " + requirements
	if check.Hint != want {
		t.Fatalf("hint = %q, want %q", check.Hint, want)
	}
}

func TestPythonProjectWithoutRequirementsAddsNoConcept(t *testing.T) {
	service := &config.Service{Type: "python", Path: t.TempDir(), Command: "python3 app.py"}
	if check, supported := pythonProjectDependencyCheck("api", service); supported {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestPythonRequirementsSetupRespectsInterpreterOwnership(t *testing.T) {
	requirements := "/workspace/api/requirements.txt"
	tests := map[string]string{
		"venv":    "python3 -m pip install -r /workspace/api/requirements.txt",
		"system":  "python3 -m pip install --user -r /workspace/api/requirements.txt",
		"managed": "python3 -m pip install --user --break-system-packages -r /workspace/api/requirements.txt",
	}
	for mode, want := range tests {
		if got := pythonRequirementsSetupCommand("python3", requirements, mode); got != want {
			t.Errorf("%s command = %q, want %q", mode, got, want)
		}
	}
}
