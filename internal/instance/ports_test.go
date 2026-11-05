package instance

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
	orbitport "github.com/iml885203/orbit/internal/port"
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
	if err := ResolvePorts(cfg, noDefaultDashboard, Survivors{}); err != nil {
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
	if err := ResolvePorts(cfg, noDefaultDashboard, Survivors{}); err != nil {
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

	got, err := ResolveDashboardPort(preferred, noDefaultDashboard)
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
	dashboard, err := ResolveDashboardPort(availableTestPort(t), noDefaultDashboard)
	if err != nil {
		t.Fatal(err)
	}
	// The daemon binds the dashboard only after resolving service ports, so
	// the port is still free when a relocated service looks for one.
	cfg := testPortConfig(dashboard)
	if err := ResolvePorts(cfg, noDefaultDashboard, Survivors{}); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services["api"].Ports["http"].Host; got == dashboard {
		t.Fatalf("service received the dashboard port %d", dashboard)
	}
}

func writeTestPortAssignments(t *testing.T, name string, ports map[string]int) {
	t.Helper()
	runtime, err := Resolve(BaseHome(), name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtime.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(portAssignments{Ports: ports})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtime.Home, "ports.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePortsMovesAServiceOffAnEarlierDashboardCollision(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ORBIT_HOME", base)
	t.Setenv(EnvBaseHome, base)
	if _, err := Activate("test-heal"); err != nil {
		t.Fatal(err)
	}
	shared := availableTestPort(t)
	cfg := testPortConfig(availableTestPort(t))
	key := configPortRequests(cfg)[0].key
	writeTestPortAssignments(t, "test-heal", map[string]int{"dashboard": shared, key: shared})

	if err := ResolvePorts(cfg, noDefaultDashboard, Survivors{}); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services["api"].Ports["http"].Host; got == shared {
		t.Fatalf("service kept the dashboard port %d an earlier run gave it", shared)
	}
}

func TestResolvePortsDoesNotPinARemovedServicePort(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ORBIT_HOME", base)
	t.Setenv(EnvBaseHome, base)
	if _, err := Activate("test-rename"); err != nil {
		t.Fatal(err)
	}
	declared := availableTestPort(t)
	writeTestPortAssignments(t, "test-rename", map[string]int{"old-name/http": declared})

	cfg := testPortConfig(declared)
	if err := ResolvePorts(cfg, noDefaultDashboard, Survivors{}); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services["api"].Ports["http"].Host; got != declared {
		t.Fatalf("renamed service got %d, want its declared %d: a removed key must not hold it", got, declared)
	}
}

func TestResolveDashboardPortAvoidsTheInstanceServicePorts(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ORBIT_HOME", base)
	t.Setenv(EnvBaseHome, base)
	if _, err := Activate("test-dash"); err != nil {
		t.Fatal(err)
	}
	service := availableTestPort(t)
	writeTestPortAssignments(t, "test-dash", map[string]int{"api/http": service})

	got, err := ResolveDashboardPort(service, noDefaultDashboard)
	if err != nil {
		t.Fatal(err)
	}
	if got == service {
		t.Fatalf("dashboard took the stopped service's port %d", service)
	}
}

func TestResolvePortsGivesASharedStoredPortToTheFirstKey(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ORBIT_HOME", base)
	t.Setenv(EnvBaseHome, base)
	if _, err := Activate("test-shared"); err != nil {
		t.Fatal(err)
	}
	shared := availableTestPort(t)
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Ports: map[string]config.PortDef{
			"admin": {Host: availableTestPort(t), Target: 81},
			"http":  {Host: availableTestPort(t), Target: 80},
		}},
	}}
	writeTestPortAssignments(t, "test-shared", map[string]int{"api/admin": shared, "api/http": shared})

	if err := ResolvePorts(cfg, noDefaultDashboard, Survivors{}); err != nil {
		t.Fatal(err)
	}
	admin, http := cfg.Services["api"].Ports["admin"].Host, cfg.Services["api"].Ports["http"].Host
	if admin != shared || http == shared {
		t.Fatalf("admin = %d, http = %d: the first key keeps shared port %d, the other moves", admin, http, shared)
	}
}

// noDefaultDashboard stands in for the default runtime's dashboard port in
// tests that are not about reserving it; port 0 is never a candidate.
const noDefaultDashboard = 0

func holdTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().(*net.TCPAddr).Port
}

func activateTestInstance(t *testing.T, name string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("ORBIT_HOME", base)
	t.Setenv(EnvBaseHome, base)
	if _, err := Activate(name); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDashboardPortMovesAStoredPortSomethingElseHolds(t *testing.T) {
	activateTestInstance(t, "test-dash-held")
	held := holdTestPort(t)
	writeTestPortAssignments(t, "test-dash-held", map[string]int{"dashboard": held})

	got, err := ResolveDashboardPort(held, noDefaultDashboard)
	if err != nil {
		t.Fatal(err)
	}
	if got == held {
		t.Fatalf("dashboard kept port %d another process holds", held)
	}
	again, err := ResolveDashboardPort(held, noDefaultDashboard)
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("next start got %d, want the recorded replacement %d", again, got)
	}
}

func TestResolveDashboardPortKeepsAFreeStoredPort(t *testing.T) {
	activateTestInstance(t, "test-dash-free")
	stored := availableTestPort(t)
	writeTestPortAssignments(t, "test-dash-free", map[string]int{"dashboard": stored})

	got, err := ResolveDashboardPort(availableTestPort(t), noDefaultDashboard)
	if err != nil {
		t.Fatal(err)
	}
	if got != stored {
		t.Fatalf("dashboard port = %d, want its stored %d", got, stored)
	}
}

func TestResolveDashboardPortSkipsTheDefaultRuntimeDashboard(t *testing.T) {
	activateTestInstance(t, "test-dash-default")
	defaultDashboard := availableTestPort(t)

	got, err := ResolveDashboardPort(defaultDashboard, defaultDashboard)
	if err != nil {
		t.Fatal(err)
	}
	if got == defaultDashboard {
		t.Fatalf("first assignment took the default runtime's dashboard port %d", got)
	}
}

// An instance first started while the default daemon was down could have
// recorded the default port; it would conflict whenever that daemon runs.
func TestResolveDashboardPortMovesAStoredDefaultRuntimeDashboard(t *testing.T) {
	activateTestInstance(t, "test-dash-stored-default")
	defaultDashboard := availableTestPort(t)
	writeTestPortAssignments(t, "test-dash-stored-default", map[string]int{"dashboard": defaultDashboard})

	got, err := ResolveDashboardPort(defaultDashboard, defaultDashboard)
	if err != nil {
		t.Fatal(err)
	}
	if got == defaultDashboard {
		t.Fatalf("dashboard kept the default runtime's port %d", got)
	}
}

func TestResolvePortsMovesAStoredPortSomethingElseHolds(t *testing.T) {
	activateTestInstance(t, "test-svc-held")
	held := holdTestPort(t)
	cfg := testPortConfig(held)
	writeTestPortAssignments(t, "test-svc-held", map[string]int{"api/http": held})

	if err := ResolvePorts(cfg, noDefaultDashboard, Survivors{}); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services["api"].Ports["http"].Host; got == held {
		t.Fatalf("service kept port %d another process holds", held)
	}
}

func TestResolvePortsKeepsAFreeStoredPort(t *testing.T) {
	activateTestInstance(t, "test-svc-free")
	stored := availableTestPort(t)
	cfg := testPortConfig(availableTestPort(t))
	writeTestPortAssignments(t, "test-svc-free", map[string]int{"api/http": stored})

	if err := ResolvePorts(cfg, noDefaultDashboard, Survivors{}); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services["api"].Ports["http"].Host; got != stored {
		t.Fatalf("service port = %d, want its stored %d", got, stored)
	}
}

func TestResolvePortsSkipsTheDefaultRuntimeDashboard(t *testing.T) {
	activateTestInstance(t, "test-svc-default")
	defaultDashboard := availableTestPort(t)
	cfg := testPortConfig(defaultDashboard)

	if err := ResolvePorts(cfg, defaultDashboard, Survivors{}); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services["api"].Ports["http"].Host; got == defaultDashboard {
		t.Fatalf("service took the default runtime's dashboard port %d", got)
	}
}

// A process the previous daemon left running still holds its port until the
// new daemon retires it; moving the port would only churn the service URL.
func TestResolvePortsKeepsAPortASurvivingProcessHolds(t *testing.T) {
	activateTestInstance(t, "test-svc-survivor")
	held := holdTestPort(t)
	cfg := testPortConfig(availableTestPort(t))
	writeTestPortAssignments(t, "test-svc-survivor", map[string]int{"api/http": held})

	survivors := Survivors{Processes: map[string]bool{"api": true}}
	if err := ResolvePorts(cfg, noDefaultDashboard, survivors); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services["api"].Ports["http"].Host; got != held {
		t.Fatalf("service port = %d, want %d its surviving process holds", got, held)
	}
}

func testContainerPortConfig(port int) *config.Config {
	return &config.Config{Containers: map[string]*config.Container{
		"db": {Ports: map[string]config.PortDef{"sql": {Host: port, Target: 1433}}},
	}}
}

// Containers outlive `orbit daemon stop`; moving a port one of them still
// publishes would make the next start replace it instead of adopting it.
func TestResolvePortsKeepsAPortASurvivingContainerPublishes(t *testing.T) {
	tests := []struct {
		name      string
		published func(held int) (map[int]bool, error)
		wantKept  bool
	}{
		{name: "published by the instance", wantKept: true, published: func(held int) (map[int]bool, error) {
			return map[int]bool{held: true}, nil
		}},
		{name: "docker unreachable", wantKept: true, published: func(int) (map[int]bool, error) {
			return nil, errors.New("docker unreachable")
		}},
		{name: "not published by the instance", wantKept: false, published: func(int) (map[int]bool, error) {
			return map[int]bool{}, nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activateTestInstance(t, "test-container-survivor")
			held := holdTestPort(t)
			cfg := testContainerPortConfig(availableTestPort(t))
			writeTestPortAssignments(t, "test-container-survivor", map[string]int{"db/sql": held})

			survivors := Survivors{ContainerPorts: func() (map[int]bool, error) { return tt.published(held) }}
			if err := ResolvePorts(cfg, noDefaultDashboard, survivors); err != nil {
				t.Fatal(err)
			}
			if got := cfg.Containers["db"].Ports["sql"].Host; (got == held) != tt.wantKept {
				t.Fatalf("container port = %d with stored %d held, want kept = %v", got, held, tt.wantKept)
			}
		})
	}
}

func TestResolvePortsSkipsContainerQueryWhenStoredPortsAreFree(t *testing.T) {
	activateTestInstance(t, "test-container-free")
	stored := availableTestPort(t)
	cfg := testContainerPortConfig(stored)
	writeTestPortAssignments(t, "test-container-free", map[string]int{"db/sql": stored})

	survivors := Survivors{ContainerPorts: func() (map[int]bool, error) {
		t.Fatal("queried Docker although every stored port was free")
		return nil, nil
	}}
	if err := ResolvePorts(cfg, noDefaultDashboard, survivors); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDashboardPortMovesAStoredPortAnotherInstanceRecorded(t *testing.T) {
	activateTestInstance(t, "test-dash-peer")
	shared := availableTestPort(t)
	writeTestPortAssignments(t, "test-dash-peer", map[string]int{"dashboard": shared})
	peer := portAssignments{UpdatedAt: time.Now().UTC(), Ports: map[string]int{"dashboard": shared}}
	peerHome := filepath.Join(BaseHome(), "instances", "peer")
	if err := os.MkdirAll(peerHome, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(peer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(peerHome, "ports.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveDashboardPort(shared, noDefaultDashboard)
	if err != nil {
		t.Fatal(err)
	}
	if got == shared {
		t.Fatalf("dashboard kept port %d another live instance recorded", shared)
	}
}
