package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestHostEnvironmentChecksResolveServiceRelativeExecutable(t *testing.T) {
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
	var commandChecks []DoctorCheck
	for _, check := range checks {
		if check.Name == "Command (api)" {
			commandChecks = append(commandChecks, check)
		}
	}
	if len(commandChecks) != 1 {
		t.Fatalf("command checks = %+v", commandChecks)
	}
	if commandChecks[0].Status != CheckPass || commandChecks[0].Message != "executable found at "+interpreter {
		t.Fatalf("check = %+v", commandChecks[0])
	}
}

func TestHostEnvironmentChecksBlockMissingCommandAndPreStartExecutables(t *testing.T) {
	project := t.TempDir()
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {
			Type:     "python",
			Path:     project,
			Command:  ".venv/bin/python app.py",
			PreStart: []string{"./scripts/prepare"},
		},
	}}

	checks := HostEnvironmentChecks(cfg)
	var failed []DoctorCheck
	for _, check := range checks {
		if check.Status == CheckFail {
			failed = append(failed, check)
		}
	}
	if len(failed) != 2 {
		t.Fatalf("failed checks = %+v", failed)
	}
	if failed[0].Name != "Command (api)" ||
		failed[0].Message != "executable not found: "+filepath.Join(project, ".venv/bin/python") ||
		failed[1].Name != "Pre-start (api #1)" ||
		failed[1].Message != "executable not found: "+filepath.Join(project, "scripts/prepare") {
		t.Fatalf("failed checks = %+v", failed)
	}
}
