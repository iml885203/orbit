package devdb

import (
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

// Regression (moved from the old cmd/orbit doctor test): the offline
// doctor's DB-workflow gate — an env without a sql-server container
// gets an informational skip, never a WORKSPACE_ROOT warn/fail.
func TestCLIDoctorChecks_UnconfiguredEnvSkips(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis", Image: "redis:7.4"},
		},
	}
	checks := CLIDoctorChecks(cfg)
	var sawSkip bool
	for _, c := range checks {
		if c.Name == "WORKSPACE_ROOT" {
			t.Errorf("unconfigured env still ran WORKSPACE_ROOT check (%s)", c.Status)
		}
		if c.Name == "DB Workflow" && c.Status == daemon.CheckInfo {
			sawSkip = true
		}
	}
	if !sawSkip {
		t.Error("expected informational 'DB Workflow' skip check")
	}
}

// An env with a sql-server container passes the gate and reports the
// workspace-root check instead of the skip.
func TestCLIDoctorChecks_ConfiguredEnvChecksRoot(t *testing.T) {
	t.Setenv("WORKSPACE_ROOT", t.TempDir())
	checks := CLIDoctorChecks(sqlServerConfig())
	if len(checks) != 1 {
		t.Fatalf("want exactly one check, got %d: %+v", len(checks), checks)
	}
	if checks[0].Name == "DB Workflow" {
		t.Errorf("configured env hit the skip gate: %+v", checks[0])
	}
}
