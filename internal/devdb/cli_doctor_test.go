package devdb

import (
	"testing"

	"github.com/iml885203/orbit/config"
)

// Regression (moved from the old cmd/orbit doctor test): the offline
// doctor's DB-workflow gate — an env without a sqlserver section
// stays silent instead of exposing an irrelevant feature concept.
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
