package devdb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

func TestPublishToolchainChecks_PrebuildAlternatives(t *testing.T) {
	tools := t.TempDir()
	if err := os.WriteFile(filepath.Join(tools, "sqlpackage"), []byte("#!/bin/sh\necho 170.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	t.Setenv("HOME", t.TempDir())

	checks := publishToolchainChecks()
	if checks[0].Status != daemon.CheckWarn || !strings.Contains(checks[0].Hint, "--dacpac-dir") {
		t.Fatalf("missing SDK check = %+v", checks[0])
	}
	if checks[1].Status != daemon.CheckPass {
		t.Fatalf("available sqlpackage check = %+v", checks[1])
	}
}

// An environment without a sqlserver section stays silent instead of
// exposing an irrelevant feature concept.
func TestCLIDoctorChecks_UnconfiguredEnvIsSilent(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis", Image: "redis:7.4"},
		},
	}
	checks := CLIDoctorChecks(cfg)
	if len(checks) != 0 {
		t.Errorf("unconfigured env reported DB checks: %+v", checks)
	}
}

// An explicitly configured env reports the workspace and publishing tools.
func TestCLIDoctorChecks_ConfiguredEnvChecksRoot(t *testing.T) {
	t.Setenv("WORKSPACE_ROOT", t.TempDir())
	checks := CLIDoctorChecks(sqlServerConfig())
	if len(checks) < 1 {
		t.Fatal("configured env returned no checks")
	}
	if checks[0].Name != "Workspace Root" {
		t.Errorf("first check = %+v, want workspace root", checks[0])
	}
}
