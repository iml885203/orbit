package instance

import (
	"net"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestResolvePortsSeparatesInstancesAndPersistsAssignments(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ORBIT_HOME", base)
	t.Setenv(EnvBaseHome, base)
	preferred := availableTestPort(t)
	firstPort := resolveTestInstancePort(t, "test-a", preferred)
	secondPort := resolveTestInstancePort(t, "test-b", preferred)
	if secondPort == firstPort {
		t.Fatalf("both instances received port %d", firstPort)
	}
	if got := resolveTestInstancePort(t, "test-a", preferred); got != firstPort {
		t.Fatalf("restart port = %d, want stable %d", got, firstPort)
	}
}

func availableTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

func resolveTestInstancePort(t *testing.T, name string, preferred int) int {
	t.Helper()
	cfg := testPortConfig(preferred)
	if _, err := Activate(name); err != nil {
		t.Fatal(err)
	}
	if err := ResolvePorts(cfg); err != nil {
		t.Fatal(err)
	}
	resolved := cfg.Services["api"].Ports["http"].Host
	if cfg.Services["api"].HealthCheck.Port != resolved {
		t.Fatalf("health port = %d, want %d", cfg.Services["api"].HealthCheck.Port, resolved)
	}
	return resolved
}

func testPortConfig(port int) *config.Config {
	return &config.Config{Services: map[string]*config.Service{
		"api": {
			Ports:       map[string]config.PortDef{"http": {Host: port, Target: port}},
			HealthCheck: &config.HealthCheckConfig{Type: "http", Port: port},
		},
	}}
}
