package daemon

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/port"
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

func TestServiceHealthCheckDoesNotSendDockerOutageToContainerLogs(t *testing.T) {
	check := serviceHealthCheck([]engine.ServiceInfo{
		{
			Name:        "cache",
			State:       engine.StateDegraded,
			StateReason: engine.DockerObservationUnavailableReason,
		},
	})

	if check.Status != CheckFail ||
		!strings.Contains(check.Hint, "Restore Docker") ||
		!strings.Contains(check.Hint, "reconnects automatically") {
		t.Fatalf("check = %+v", check)
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

func TestLiveServiceHealthChecksRevalidatePortConflict(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	portNumber := listener.Addr().(*net.TCPAddr).Port
	service := engine.ServiceInfo{
		Name:  "api",
		State: engine.StateDegraded,
		PortConflict: port.NewConflictError(port.Conflict{
			Port:    portNumber,
			Service: "api",
		}),
	}

	occupied := liveServiceHealthChecks([]engine.ServiceInfo{service})
	if len(occupied) != 2 || occupied[0].Name != "Port "+strconv.Itoa(portNumber) ||
		occupied[0].Status != CheckFail || !strings.Contains(occupied[0].Hint, "run: ") {
		t.Fatalf("occupied checks = %+v", occupied)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	released := liveServiceHealthChecks([]engine.ServiceInfo{service})
	if len(released) != 2 || released[0].Status != CheckPass ||
		!strings.Contains(released[0].Message, "previous conflict resolved") {
		t.Fatalf("released port check = %+v", released)
	}
	if released[1].Status != CheckPass || released[1].Hint != "run: orbit up api" {
		t.Fatalf("released health check = %+v", released[1])
	}
}
