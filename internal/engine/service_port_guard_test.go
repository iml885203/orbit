package engine

import (
	"net"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/port"
)

func TestEnsureServicePortsAvailableRejectsForeignListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	port := listener.Addr().(*net.TCPAddr).Port
	service := &config.Service{
		Ports: map[string]config.PortDef{"http": {Host: port, Target: port}},
	}

	err = ensureServicePortsAvailable("api", service)
	if err == nil {
		t.Fatal("foreign listener accepted")
	}
	for _, evidence := range []string{strconv.Itoa(port), "already in use", "api"} {
		if !strings.Contains(err.Error(), evidence) {
			t.Errorf("error %q missing %q", err, evidence)
		}
	}
}

func TestEnsureServicePortsAvailableAcceptsFreePort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	service := &config.Service{
		Ports: map[string]config.PortDef{"http": {Host: port, Target: port}},
	}

	if err := ensureServicePortsAvailable("api", service); err != nil {
		t.Fatalf("free port rejected: %v", err)
	}
}

func TestServicePortConflictErrorIncludesOwnerAndSafeInspection(t *testing.T) {
	err := port.NewConflictError(port.Conflict{Service: "api", Port: 8080, PID: "1234", Process: "/usr/bin/python3"})
	for _, evidence := range []string{"8080", "api", "python3", "1234"} {
		if !strings.Contains(err.Error(), evidence) {
			t.Errorf("error %q missing %q", err, evidence)
		}
	}
	if !strings.Contains(err.InspectCommand, "1234") {
		t.Errorf("inspect command = %q", err.InspectCommand)
	}
	if strings.Contains(strings.ToLower(err.Error()), "kill") {
		t.Errorf("error should not suggest killing a process: %q", err)
	}
}

func TestServicePortConflictErrorIncludesPlatformInspectionWhenOwnerUnknown(t *testing.T) {
	err := port.NewConflictError(port.Conflict{Service: "api", Port: 8080, PID: "?", Process: "?"})
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(err.InspectCommand, "lsof") {
			t.Errorf("macOS inspection should use lsof: %q", err.InspectCommand)
		}
	case "windows":
		if !strings.Contains(err.InspectCommand, "Get-NetTCPConnection") {
			t.Errorf("Windows inspection should use PowerShell: %q", err.InspectCommand)
		}
	default:
		if !strings.Contains(err.InspectCommand, "ss -ltnp") {
			t.Errorf("Linux inspection should use ss: %q", err.InspectCommand)
		}
	}
}
