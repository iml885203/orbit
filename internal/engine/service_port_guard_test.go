package engine

import (
	"net"
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
