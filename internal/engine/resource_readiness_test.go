package engine

import (
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

func TestResourceReadinessUsesOneDeclaredEndpointWithoutExtraConfig(t *testing.T) {
	ports := map[string]config.PortDef{
		"database": {Host: 25432, Target: 5432},
	}

	got := resourceReadinessCheck(nil, ports, 2*time.Second)

	if got == nil {
		t.Fatal("readiness check was not inferred")
	}
	if got.Type != "tcp" || got.Port != 25432 {
		t.Fatalf("readiness check = %#v, want TCP on selected host port 25432", got)
	}
	if got.Interval != 2*time.Second ||
		got.Timeout != 5*time.Second ||
		got.Retries != config.DefaultHealthRetries ||
		got.FailureThreshold != config.DefaultHealthFailureThreshold {
		t.Fatalf("readiness defaults = %#v", got)
	}
}

func TestResourceReadinessPreservesExplicitApplicationProbe(t *testing.T) {
	explicit := &config.HealthCheckConfig{
		Type: "http",
		Port: 28080,
		Path: "/ready",
	}

	got := resourceReadinessCheck(
		explicit,
		map[string]config.PortDef{"http": {Host: 29090}},
		time.Second,
	)

	if got != explicit {
		t.Fatalf("readiness check = %#v, want explicit probe %#v", got, explicit)
	}
}

func TestReadinessCheckUsesTheActiveModeForDualDefinedResource(t *testing.T) {
	cfg := &config.Config{
		Settings: config.RuntimeSettings{HealthCheckInterval: time.Second},
		Containers: map[string]*config.Container{
			"api": {
				Ports: map[string]config.PortDef{
					"container": {Host: 29090},
				},
			},
		},
		Services: map[string]*config.Service{
			"api": {
				Ports: map[string]config.PortDef{
					"dev": {Host: 28080},
				},
			},
		},
	}

	dev := readinessCheckForResource(cfg, "api", "service")
	container := readinessCheckForResource(cfg, "api", "container")

	if dev == nil || dev.Port != 28080 {
		t.Fatalf("dev readiness = %#v, want port 28080", dev)
	}
	if container == nil || container.Port != 29090 {
		t.Fatalf("container readiness = %#v, want port 29090", container)
	}
}
