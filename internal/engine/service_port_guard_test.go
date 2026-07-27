package engine

import (
	"net"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
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
	for _, evidence := range []string{strconv.Itoa(port), "already in use", "api", "inspect"} {
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
	err := servicePortConflictError("api", 8080, "1234", "/usr/bin/python3")
	for _, evidence := range []string{"8080", "api", "python3", "1234", processInspectCommand("1234")} {
		if !strings.Contains(err.Error(), evidence) {
			t.Errorf("error %q missing %q", err, evidence)
		}
	}
	if strings.Contains(strings.ToLower(err.Error()), "kill") {
		t.Errorf("error should not suggest killing a process: %q", err)
	}
}

func TestServicePortConflictErrorIncludesPlatformInspectionWhenOwnerUnknown(t *testing.T) {
	err := servicePortConflictError("api", 8080, "?", "?")
	if !strings.Contains(err.Error(), portInspectCommand(8080)) {
		t.Errorf("error %q missing platform inspection command", err)
	}
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(err.Error(), "lsof") {
			t.Errorf("macOS error should use lsof: %q", err)
		}
	case "windows":
		if !strings.Contains(err.Error(), "Get-NetTCPConnection") {
			t.Errorf("Windows error should use PowerShell: %q", err)
		}
	default:
		if !strings.Contains(err.Error(), "ss -ltnp") {
			t.Errorf("Linux error should use ss: %q", err)
		}
	}
}
