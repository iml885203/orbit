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

func TestSQLServerReadinessChecks(t *testing.T) {
	for _, tc := range []struct {
		name       string
		health     *config.HealthCheckConfig
		wantWarn   bool
		wantPhrase string
	}{
		{name: "implicit tcp", wantWarn: true, wantPhrase: "has no explicit health check"},
		{name: "explicit tcp", health: &config.HealthCheckConfig{Type: "tcp"}, wantWarn: true, wantPhrase: "uses a tcp health check"},
		{name: "exec proves login", health: &config.HealthCheckConfig{Type: "exec"}},
		{name: "other explicit probe", health: &config.HealthCheckConfig{Type: "healthcheck"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := sqlServerConfig()
			cfg.Containers["sql-server"].HealthCheck = tc.health
			section := SQLServerFrom(cfg)
			section.Username = "orbit_user"
			section.PasswordEnv = "ORBIT_DB_PASSWORD"
			checks := sqlServerReadinessChecks(cfg)
			if tc.wantWarn != (len(checks) == 1) {
				t.Fatalf("checks = %+v, want warning = %v", checks, tc.wantWarn)
			}
			if !tc.wantWarn {
				return
			}
			check := checks[0]
			if check.Name != "SQL Server Readiness" || check.Status != daemon.CheckWarn {
				t.Errorf("check = %+v, want named warning", check)
			}
			if !strings.Contains(check.Message, tc.wantPhrase) {
				t.Errorf("message = %q, want %q", check.Message, tc.wantPhrase)
			}
			for _, want := range []string{"containers.sql-server.health_check", "/opt/mssql-tools18/bin/sqlcmd", "orbit_user", "ORBIT_DB_PASSWORD", "SELECT 1", "SQLCMDPASSWORD", "is empty"} {
				if !strings.Contains(check.Hint, want) {
					t.Errorf("hint = %q, want %q", check.Hint, want)
				}
			}
			if strings.Contains(check.Hint, " -P ") {
				t.Errorf("hint exposes the password through sqlcmd argv: %q", check.Hint)
			}
		})
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
	foundReadiness := false
	for _, check := range checks {
		if check.Name == "SQL Server Readiness" && check.Status == daemon.CheckWarn {
			foundReadiness = true
		}
	}
	if !foundReadiness {
		t.Errorf("configured CLI doctor checks omit SQL Server readiness warning: %+v", checks)
	}
}
