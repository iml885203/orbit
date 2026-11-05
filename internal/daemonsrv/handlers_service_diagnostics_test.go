//go:build !windows

package daemonsrv

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/engine"
)

// These tests start a real host process whose readiness probe targets an
// h2c-only server, the shape of the service that stayed in "starting" in
// #207, and read the status a stuck `orbit up` is diagnosed from.

func TestStatusShowsFirstProbeInFlightWhileStarting(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var once sync.Once
	port := startH2COnlyServer(t, http.StatusOK, func() {
		once.Do(func() { close(entered) })
		<-release
	})
	srv, cfg := startProbedService(t, port, 2)

	waitFor(t, entered, "first probe to reach the server")
	got := statusOf(t, srv, cfg)
	if got.State != "starting" || got.StartingPhase != engine.StartingPhaseProbing {
		t.Fatalf("state = %q phase = %q, want starting/probing", got.State, got.StartingPhase)
	}
	if got.PID <= 0 {
		t.Fatalf("pid = %d, want the managed process pid", got.PID)
	}
	hp := got.HealthProgress
	if hp == nil || hp.ProbeStartedAt == nil || hp.Attempts != 0 || hp.LastProbeAt != nil {
		t.Fatalf("health_progress = %+v, want a first probe in flight", hp)
	}
}

func TestStatusKeepsProbeEvidenceOnceSettled(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		wantState string
		wantErr   string
	}{
		{name: "healthy", status: http.StatusOK, wantState: "healthy"},
		{name: "degraded", status: http.StatusServiceUnavailable, wantState: "degraded", wantErr: "HTTP 503"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := startH2COnlyServer(t, tc.status, func() {})
			srv, cfg := startProbedService(t, port, 1)

			got := waitForStatus(t, srv, cfg, func(s ResourceStatus) bool { return s.State == tc.wantState })
			if got.StartingPhase != "" {
				t.Fatalf("starting_phase = %q outside starting", got.StartingPhase)
			}
			if got.PID <= 0 {
				t.Fatalf("pid = %d, want the managed process pid", got.PID)
			}
			hp := got.HealthProgress
			if hp == nil || hp.LastProbeAt == nil || hp.Protocol != "h2c" || !strings.Contains(hp.LastErr, tc.wantErr) {
				t.Fatalf("health_progress = %+v, want h2c evidence with last_err containing %q", hp, tc.wantErr)
			}
		})
	}
}

// startProbedService runs the daemon's real lifecycle for one host process
// probed over HTTP on port.
func startProbedService(t *testing.T, port, retries int) (*Server, *config.Config) {
	t.Helper()
	cfg := &config.Config{
		Containers: map[string]*config.Container{},
		Services: map[string]*config.Service{
			"basket-api": {
				Name:    "basket-api",
				Type:    "node",
				Path:    t.TempDir(),
				Command: "sleep 30",
				HealthCheck: &config.HealthCheckConfig{
					Type: "http", Port: port, Path: "/health",
					Interval: time.Second, Timeout: 5 * time.Second, Retries: retries,
				},
			},
		},
	}
	srv := newTestServer(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		_ = srv.app.Orchestrator.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-loopDone
		_ = srv.app.ProcessMgr.Stop("basket-api", time.Second)
	})
	srv.app.Orchestrator.Start([]string{"basket-api"})
	return srv, cfg
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// startH2COnlyServer mirrors the health package's startH2CServer, minus
// HTTP/1 support, so probes must settle on h2c.
func startH2COnlyServer(t *testing.T, status int, onRequest func()) int {
	t.Helper()
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	protocols := &http.Protocols{}
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{
		Protocols: protocols,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			onRequest()
			w.WriteHeader(status)
		}),
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return listener.Addr().(*net.TCPAddr).Port
}

func statusOf(t *testing.T, srv *Server, cfg *config.Config) ResourceStatus {
	t.Helper()
	statuses := srv.computeStatuses(cfg)
	for i := range statuses {
		if statuses[i].Name == "basket-api" {
			return statuses[i]
		}
	}
	t.Fatal("basket-api missing from status")
	return ResourceStatus{}
}

func waitForStatus(t *testing.T, srv *Server, cfg *config.Config, done func(ResourceStatus) bool) ResourceStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		status := statusOf(t, srv, cfg)
		if done(status) {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("status never settled: %+v (health_progress %+v)", status, status.HealthProgress)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
