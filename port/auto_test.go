package port

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestResolveAutoPortsMovesOccupiedPreferencesAndUpdatesReadiness(t *testing.T) {
	occupied := listenOnAvailablePort(t)
	preferred := occupied.Addr().(*net.TCPAddr).Port
	path := filepath.Join(t.TempDir(), "env.yaml")
	source := fmt.Sprintf(`version: "2"
containers:
  cache:
    image: redis:7.4-alpine
    ports:
      redis: "${ORBIT_AUTO_PORT_RESOLVE_TEST_CACHE:-%d}:6379"
    health_check:
      type: tcp
      port: %d
services:
  api:
    type: python
    path: .
    command: python3 app.py
    url: http://localhost:%d
    ports:
      http: "${ORBIT_AUTO_PORT_RESOLVE_TEST_API:-%d}"
    health_check:
      type: http
      path: /health
      port: %d
`, preferred, preferred, preferred, preferred, preferred)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	runtimeReserved := preferred + 1
	resolutions, err := ResolveAutoPorts(cfg, nil, runtimeReserved)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolutions) != 2 {
		t.Fatalf("resolutions = %+v, want two moved ports", resolutions)
	}
	cachePort := cfg.Containers["cache"].Ports["redis"].Host
	apiPort := cfg.Services["api"].Ports["http"].Host
	if cachePort == preferred || apiPort == preferred || cachePort == apiPort ||
		cachePort == runtimeReserved || apiPort == runtimeReserved {
		t.Fatalf("resolved cache=%d api=%d preferred=%d reserved=%d", cachePort, apiPort, preferred, runtimeReserved)
	}
	if cfg.Containers["cache"].HealthCheck.Port != cachePort {
		t.Fatalf("cache health port = %d, want %d", cfg.Containers["cache"].HealthCheck.Port, cachePort)
	}
	if cfg.Services["api"].HealthCheck.Port != apiPort {
		t.Fatalf("api health port = %d, want %d", cfg.Services["api"].HealthCheck.Port, apiPort)
	}
	if cfg.Services["api"].URL != fmt.Sprintf("http://localhost:%d", apiPort) {
		t.Fatalf("api URL = %q", cfg.Services["api"].URL)
	}
}

func TestResolveAutoPortsPreservesManagedContainerChoice(t *testing.T) {
	existing := listenOnAvailablePort(t)
	existingPort := existing.Addr().(*net.TCPAddr).Port
	preferredListener := listenOnAvailablePort(t)
	preferred := preferredListener.Addr().(*net.TCPAddr).Port
	path := filepath.Join(t.TempDir(), "env.yaml")
	source := fmt.Sprintf(`version: "2"
containers:
  cache:
    image: redis:7.4-alpine
    ports:
      redis: "${ORBIT_AUTO_PORT_RESOLVE_TEST_EXISTING:-%d}:6379"
`, preferred)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	resolutions, err := ResolveAutoPorts(cfg, func(name string, target int) (int, bool, error) {
		if name != "cache" || target != 6379 {
			t.Fatalf("existing lookup = %s:%d", name, target)
		}
		return existingPort, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Containers["cache"].Ports["redis"].Host; got != existingPort {
		t.Fatalf("resolved port = %d, want managed %d", got, existingPort)
	}
	if len(resolutions) != 1 || resolutions[0].Actual != existingPort {
		t.Fatalf("resolutions = %+v", resolutions)
	}
}

func listenOnAvailablePort(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}
