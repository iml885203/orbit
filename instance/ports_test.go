package instance

import (
	"net"
	"strconv"
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

func TestResolvePortsUpdatesCanonicalApplicationURL(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ORBIT_HOME", base)
	t.Setenv(EnvBaseHome, base)
	preferred := availableTestPort(t)
	blocker, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(preferred)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Close() }()
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"store-front": {
				URL:   "http://LOCALHOST:" + strconv.Itoa(preferred) + "/admin?tab=jobs#active",
				Ports: map[string]config.PortDef{"web": {Host: preferred, Target: 80}},
			},
		},
		Services: map[string]*config.Service{
			"api": {
				URL:   "http://127.0.0.1:" + strconv.Itoa(preferred) + "/docs",
				Ports: map[string]config.PortDef{"http": {Host: preferred, Target: preferred}},
			},
		},
	}
	if _, err := Activate("url-remap"); err != nil {
		t.Fatal(err)
	}
	if err := ResolvePorts(cfg); err != nil {
		t.Fatal(err)
	}

	resolved := cfg.Containers["store-front"].Ports["web"].Host
	if resolved == preferred {
		t.Fatalf("container port remained blocked preference %d", preferred)
	}
	want := "http://LOCALHOST:" + strconv.Itoa(resolved) + "/admin?tab=jobs#active"
	if got := cfg.Containers["store-front"].ResolveURL(); got != want {
		t.Fatalf("container URL = %q, want %q", got, want)
	}
	servicePort := cfg.Services["api"].Ports["http"].Host
	serviceWant := "http://127.0.0.1:" + strconv.Itoa(servicePort) + "/docs"
	if got := cfg.Services["api"].ResolveURL(); got != serviceWant {
		t.Fatalf("service URL = %q, want %q", got, serviceWant)
	}
}

func TestRemapApplicationURLLeavesUnrelatedEndpointsUnchanged(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "remote endpoint", url: "https://example.com/app"},
		{name: "different scheme", url: "https://localhost:8443/app"},
		{name: "different declared port", url: "http://localhost:9090/app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := config.RemapApplicationURL(tt.url, 8080, 18080); got != tt.url {
				t.Fatalf("remapped URL = %q, want unchanged %q", got, tt.url)
			}
		})
	}
}

func TestRemapApplicationURLSupportsIPv6Loopback(t *testing.T) {
	got := config.RemapApplicationURL("http://[::1]:8080/app", 8080, 18080)
	if got != "http://[::1]:18080/app" {
		t.Fatalf("remapped URL = %q, want IPv6 loopback endpoint", got)
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
