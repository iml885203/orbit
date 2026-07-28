package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestConfiguredPythonInterpreterUsesServiceRelativePath(t *testing.T) {
	project := t.TempDir()
	interpreter := filepath.Join(project, ".venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(interpreter), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(interpreter, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "python", Path: project, Command: ".venv/bin/python app.py"},
	}}

	checks := HostEnvironmentChecks(cfg)
	var pythonChecks []DoctorCheck
	for _, check := range checks {
		if check.Name == "Python" || check.Name == "Python (api)" {
			pythonChecks = append(pythonChecks, check)
		}
	}
	if len(pythonChecks) != 1 {
		t.Fatalf("python checks = %+v", pythonChecks)
	}
	if pythonChecks[0].Status != CheckPass || pythonChecks[0].Message != "configured interpreter found at "+interpreter {
		t.Fatalf("check = %+v", pythonChecks[0])
	}
}

func TestConfiguredPythonInterpreterNamesMissingPath(t *testing.T) {
	project := t.TempDir()
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "python", Path: project, Command: ".venv/bin/python app.py"},
	}}

	checks := configuredPythonInterpreterChecks(cfg)
	if len(checks) != 1 || checks[0].Status != CheckFail {
		t.Fatalf("checks = %+v", checks)
	}
	want := filepath.Join(project, ".venv", "bin", "python")
	if checks[0].Message != "configured interpreter not found: "+want {
		t.Fatalf("message = %q", checks[0].Message)
	}
}
