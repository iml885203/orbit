package instance

import (
	"net"
	"strconv"
	"testing"

	"github.com/iml885203/orbit/config"
	orbitport "github.com/iml885203/orbit/port"
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

// A listener the loopback-only probe misses — here IPv6 loopback — still
// makes the start-time conflict check reject the port, so allocation must
// skip it rather than hand out a port the instance cannot start on.
func TestResolvePortsSkipsPortTheStartCheckWouldReject(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	held := listener.Addr().(*net.TCPAddr).Port
	base := t.TempDir()
	t.Setenv("ORBIT_HOME", base)
	t.Setenv(EnvBaseHome, base)

	got := resolveTestInstancePort(t, "test-a", held)

	if got == held {
		t.Fatalf("allocated port %d, which is held on [::1]", held)
	}
	if !orbitport.Available(got) {
		t.Fatalf("allocated port %d is not available to the start-time check", got)
	}
}

// The dashboard only binds loopback, so a listener the start-time check
// would object to elsewhere must not move it off its preferred port.
func TestResolveDashboardPortIgnoresListenerOutsideIPv4Loopback(t *testing.T) {
	preferred := ipv6OnlyHeldPort(t)
	base := t.TempDir()
	t.Setenv("ORBIT_HOME", base)
	t.Setenv(EnvBaseHome, base)
	if _, err := Activate("test-a"); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveDashboardPort(preferred)
	if err != nil {
		t.Fatal(err)
	}
	if got != preferred {
		t.Fatalf("dashboard port = %d, want preferred %d", got, preferred)
	}
}

// ipv6OnlyHeldPort holds a port on [::1] that is still free on 127.0.0.1, so
// only a probe that covers IPv6 sees it as taken.
func ipv6OnlyHeldPort(t *testing.T) int {
	t.Helper()
	for attempt := 0; attempt < 20; attempt++ {
		listener, err := net.Listen("tcp6", "[::1]:0")
		if err != nil {
			t.Skipf("IPv6 loopback unavailable: %v", err)
		}
		held := listener.Addr().(*net.TCPAddr).Port
		probe, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(held)))
		if err != nil {
			_ = listener.Close()
			continue
		}
		_ = probe.Close()
		t.Cleanup(func() { _ = listener.Close() })
		return held
	}
	t.Fatal("no port free on 127.0.0.1 while held on [::1]")
	return 0
}

func TestResolvePortsNeverHandsOutTheInstanceDashboardPort(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ORBIT_HOME", base)
	t.Setenv(EnvBaseHome, base)
	if _, err := Activate("test-dashboard"); err != nil {
		t.Fatal(err)
	}
	dashboard, err := ResolveDashboardPort(availableTestPort(t))
	if err != nil {
		t.Fatal(err)
	}
	// The daemon binds the dashboard only after resolving service ports, so
	// the port is still free when a relocated service looks for one.
	cfg := testPortConfig(dashboard)
	if err := ResolvePorts(cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services["api"].Ports["http"].Host; got == dashboard {
		t.Fatalf("service received the dashboard port %d", dashboard)
	}
}
