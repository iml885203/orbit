package daemon

import (
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestDependencyReadinessWarnsOnlyWhenOrbitCannotInferAProbe(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"database": {
				Ports: map[string]config.PortDef{
					"primary": {Host: 5432},
					"admin":   {Host: 5433},
				},
			},
			"cache": {
				Ports: map[string]config.PortDef{
					"redis": {Host: 6379},
				},
			},
		},
		Services: map[string]*config.Service{
			"api": {DependsOn: []string{"database", "cache"}},
		},
	}

	checks := DependencyReadinessChecks(cfg)

	if len(checks) != 1 {
		t.Fatalf("readiness checks = %#v, want one ambiguous dependency warning", checks)
	}
	check := checks[0]
	if check.Name != "Readiness (database)" ||
		check.Status != CheckWarn ||
		check.Hint != "Add containers.database.health_check so dependents wait for a real readiness signal" {
		t.Fatalf("readiness check = %#v", check)
	}
}

func TestDependencyReadinessAcceptsExplicitProbeForPortlessContainer(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"worker-runtime": {
				HealthCheck: &config.HealthCheckConfig{
					Type:    "log",
					Pattern: "ready",
				},
			},
		},
		Services: map[string]*config.Service{
			"api": {DependsOn: []string{"worker-runtime"}},
		},
	}

	if checks := DependencyReadinessChecks(cfg); len(checks) != 0 {
		t.Fatalf("readiness checks = %#v, want explicit probe accepted", checks)
	}
}
