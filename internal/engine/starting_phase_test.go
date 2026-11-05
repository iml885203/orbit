package engine

import (
	"context"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
)

func TestStartingPhaseSeparatesLaunchFromReadinessWait(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api"}}}
	o := newTestOrchestrator(cfg)
	release := make(chan struct{})
	o.OnStartProcess = func(context.Context, string, int, *config.Config, *config.Service) error {
		<-release
		return nil
	}
	probing := make(chan struct{})
	o.OnHealthCheck = func(context.Context, string, int) error {
		close(probing)
		return nil
	}

	if err := o.startService(context.Background(), "api"); err != nil {
		t.Fatalf("startService: %v", err)
	}
	if got := serviceInfo(t, o, "api").StartingPhase(); got != StartingPhaseLaunching {
		t.Fatalf("phase while start hook runs = %q, want %q", got, StartingPhaseLaunching)
	}

	close(release)
	waitClosed(t, probing, "health check after launch")
	if got := serviceInfo(t, o, "api").StartingPhase(); got != StartingPhaseProbing {
		t.Fatalf("phase after launch = %q, want %q", got, StartingPhaseProbing)
	}

	o.mu.Lock()
	o.services["api"].Transition(StateHealthy)
	o.mu.Unlock()
	if got := serviceInfo(t, o, "api").StartingPhase(); got != "" {
		t.Fatalf("phase outside starting = %q, want empty", got)
	}
}

func TestStartingPhaseIgnoresALaunchFromASupersededStart(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api"}}}
	o := newTestOrchestrator(cfg)
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstReturned := make(chan struct{})
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	o.OnStartProcess = func(_ context.Context, _ string, generation int, _ *config.Config, _ *config.Service) error {
		if generation == 1 {
			close(firstEntered)
			<-releaseFirst
			return nil
		}
		<-block
		return nil
	}
	o.OnHealthCheck = func(_ context.Context, _ string, generation int) error {
		if generation == 1 {
			close(firstReturned)
		}
		return nil
	}

	if err := o.startService(context.Background(), "api"); err != nil {
		t.Fatalf("first start: %v", err)
	}
	waitClosed(t, firstEntered, "first start hook")
	// A restart supersedes generation 1 while its start hook still runs.
	o.mu.Lock()
	o.services["api"].Transition(StateStopped)
	o.mu.Unlock()
	if err := o.startService(context.Background(), "api"); err != nil {
		t.Fatalf("second start: %v", err)
	}
	close(releaseFirst)
	waitClosed(t, firstReturned, "first start to finish launching")

	if got := serviceInfo(t, o, "api").StartingPhase(); got != StartingPhaseLaunching {
		t.Fatalf("phase = %q, want %q while the current start is still launching", got, StartingPhaseLaunching)
	}
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func serviceInfo(t *testing.T, o *Orchestrator, name string) *ServiceInfo {
	t.Helper()
	info, ok := o.GetServiceInfo(name)
	if !ok {
		t.Fatalf("service %q unknown", name)
	}
	return &info
}
