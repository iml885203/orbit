package daemon

import (
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/engine"
)

func TestServiceHealthCheckReportsDegradedEvidence(t *testing.T) {
	check := serviceHealthCheck([]engine.ServiceInfo{
		{Name: "cache", State: engine.StateHealthy},
		{Name: "api", State: engine.StateDegraded, StateReason: "exited: status 1"},
	})

	if check.Status != CheckFail {
		t.Fatalf("status = %q, want %q", check.Status, CheckFail)
	}
	if !strings.Contains(check.Message, "api — exited: status 1") {
		t.Fatalf("message = %q, want service and reason", check.Message)
	}
	if check.Hint != "run: orbit logs api" {
		t.Fatalf("hint = %q", check.Hint)
	}
}

func TestServiceHealthCheckTreatsStoppedAsIntentional(t *testing.T) {
	check := serviceHealthCheck([]engine.ServiceInfo{
		{Name: "cache", State: engine.StateHealthy},
		{Name: "worker", State: engine.StateStopped},
	})

	if check.Status != CheckPass {
		t.Fatalf("status = %q, want %q", check.Status, CheckPass)
	}
	if check.Message != "1 healthy, 1 stopped" {
		t.Fatalf("message = %q", check.Message)
	}
}

func TestServiceHealthCheckPointsStoppedEnvironmentToUp(t *testing.T) {
	check := serviceHealthCheck([]engine.ServiceInfo{
		{Name: "cache", State: engine.StateStopped},
		{Name: "worker", State: engine.StateStopped},
	})

	if check.Status != CheckPass || check.Hint != "run: orbit up" {
		t.Fatalf("check = %+v", check)
	}
	if check.Message != "All stopped (0/2)" {
		t.Fatalf("message = %q", check.Message)
	}
}

func TestServiceHealthCheckWarnsWhileConverging(t *testing.T) {
	check := serviceHealthCheck([]engine.ServiceInfo{
		{Name: "api", State: engine.StateStarting},
	})

	if check.Status != CheckWarn {
		t.Fatalf("status = %q, want %q", check.Status, CheckWarn)
	}
	if !strings.Contains(check.Message, "api (starting)") {
		t.Fatalf("message = %q", check.Message)
	}
}
