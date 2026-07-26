package engine

import (
	"context"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

// These tests drive the "full-enumeration" refactor:
//   - NewOrchestrator enumerates every service in config; no 'enabled' subset.
//   - Everything starts in StateStopped until something asks to start it.
//   - Run() does not auto-start anything.
//   - Start(names) transitions stopped → pending → starting (respecting deps).
//   - OnContainerSeen adopts externally-running containers as healthy and
//     detects drift when they disappear.

func twoContainerCfg() *config.Config {
	return &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis"},
			"kafka": {Name: "kafka"},
		},
		Services: map[string]*config.Service{
			"api": {Name: "api", DependsOn: []string{"redis"}},
		},
	}
}

func newTestOrchestrator(cfg *config.Config) *Orchestrator {
	o := NewOrchestrator(config.NewHolder(cfg), nil, nil)
	o.OnStartContainer = func(_ context.Context, _ string, _ *config.Container) error { return nil }
	o.OnStopContainer = func(_ context.Context, _ string) error { return nil }
	o.OnStartProcess = func(_ context.Context, _ string, _ int, _ *config.Config, _ *config.Service) error { return nil }
	o.OnStopProcess = func(_ string) error { return nil }
	o.OnHealthCheck = func(_ context.Context, _ string, _ int) error { return nil }
	return o
}

func TestNewOrchestrator_EnumeratesEveryService(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	all := o.GetAllServices()
	if len(all) != 3 {
		t.Fatalf("GetAllServices len = %d, want 3 (redis, kafka, api)", len(all))
	}
	names := map[string]bool{}
	for _, s := range all {
		names[s.Name] = true
	}
	for _, want := range []string{"redis", "kafka", "api"} {
		if !names[want] {
			t.Errorf("missing service %q in enumeration", want)
		}
	}
}

func TestNewOrchestrator_InitialStateIsStopped(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	for _, s := range o.GetAllServices() {
		if s.State != StateStopped {
			t.Errorf("%s initial state = %s, want stopped", s.Name, s.State)
		}
	}
}

func TestRun_DoesNotAutoStartAnything(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	started := map[string]bool{}
	o.OnStartContainer = func(_ context.Context, name string, _ *config.Container) error {
		started[name] = true
		return nil
	}
	o.OnStartProcess = func(_ context.Context, name string, _ int, _ *config.Config, _ *config.Service) error {
		started[name] = true
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = o.Run(ctx) }()

	time.Sleep(80 * time.Millisecond)
	cancel()

	if len(started) != 0 {
		t.Errorf("Run() auto-started services %v; should have started none", started)
	}
	for _, s := range o.GetAllServices() {
		if s.State != StateStopped {
			t.Errorf("%s state = %s, want stopped (Run should not transition)", s.Name, s.State)
		}
	}
}

func TestStart_TransitionsFromStoppedToStarting(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	startedCh := make(chan string, 4)
	o.OnStartContainer = func(_ context.Context, name string, _ *config.Container) error {
		startedCh <- name
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = o.Run(ctx) }()

	o.Start([]string{"redis"})

	select {
	case name := <-startedCh:
		if name != "redis" {
			t.Errorf("OnStartContainer called with %q, want redis", name)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("OnStartContainer never called for redis")
	}
}

func TestStart_SkipsAlreadyHealthy(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	o.MarkServiceHealthy("redis")

	starts := 0
	o.OnStartContainer = func(_ context.Context, _ string, _ *config.Container) error {
		starts++
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = o.Run(ctx) }()

	o.Start([]string{"redis"})
	time.Sleep(80 * time.Millisecond)

	if starts != 0 {
		t.Errorf("OnStartContainer called %d times, want 0 (already healthy)", starts)
	}
}

func TestStart_WaitsForPendingDeps(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	started := make(chan string, 4)
	o.OnStartContainer = func(_ context.Context, name string, _ *config.Container) error {
		started <- name
		return nil
	}
	o.OnStartProcess = func(_ context.Context, name string, _ int, _ *config.Config, _ *config.Service) error {
		started <- name
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = o.Run(ctx) }()

	o.Start([]string{"api", "redis"})

	got := map[string]bool{}
	deadline := time.After(300 * time.Millisecond)
collect:
	for len(got) < 2 {
		select {
		case name := <-started:
			got[name] = true
			// redis becoming healthy unblocks api. Start() bumped redis
			// to generation 1, so the injected event must match it.
			if name == "redis" {
				o.events <- Event{Type: EventHealthOK, Service: "redis", Generation: 1}
			}
		case <-deadline:
			break collect
		}
	}

	if !got["redis"] {
		t.Error("redis never started")
	}
	if !got["api"] {
		t.Error("api never started — dep signalling broken")
	}
}

func TestOnContainerSeen_AdoptsAsHealthy(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	o.OnContainerSeen("redis", true)

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateHealthy {
		t.Errorf("redis state = %s, want healthy after OnContainerSeen(running=true)", info.State)
	}
}

func TestOnContainerSeen_DriftFromHealthyToDegraded(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	o.OnContainerSeen("redis", true)
	o.OnContainerSeen("redis", false)

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateDegraded {
		t.Errorf("redis state = %s, want degraded after drift", info.State)
	}
	if info.StateReason == "" {
		t.Error("redis StateReason empty, want a drift explanation")
	}
}

func TestOnContainerSeen_NotRunningFromStoppedStaysStopped(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	o.OnContainerSeen("redis", false)

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateStopped {
		t.Errorf("redis state = %s, want stopped (no drift if never was running)", info.State)
	}
}

func TestOnContainerGone_StopFailureReconcilesToStopped(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	// A stop whose docker delete outlived its ctx: StopService parked the
	// container in degraded, then the delete completed and the poller no
	// longer sees the container at all.
	o.OnContainerSeen("redis", true)
	o.mu.Lock()
	o.services["redis"].Transition(StateDegraded)
	o.services["redis"].StateReason = "stop failed: context deadline exceeded"
	o.mu.Unlock()

	o.OnContainerGone("redis")

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateStopped {
		t.Errorf("redis state = %s, want stopped after container vanished", info.State)
	}
	if info.StateReason != "" {
		t.Errorf("redis StateReason = %q, want cleared", info.StateReason)
	}
}

func TestOnContainerGone_StoppingReconcilesToStopped(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	o.OnContainerSeen("redis", true)
	o.mu.Lock()
	o.services["redis"].Transition(StateStopping)
	o.mu.Unlock()

	o.OnContainerGone("redis")

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateStopped {
		t.Errorf("redis state = %s, want stopped", info.State)
	}
}

func TestOnContainerGone_HealthyDriftsToDegraded(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	o.OnContainerSeen("redis", true)
	o.OnContainerGone("redis")

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateDegraded {
		t.Errorf("redis state = %s, want degraded when a healthy container vanishes", info.State)
	}
	if info.StateReason == "" {
		t.Error("redis StateReason empty, want a removal explanation")
	}
}

func TestOnContainerGone_StoppedAndUnknownIgnored(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	// Stopped container missing from a poll is the normal pre-start state.
	o.OnContainerGone("redis")
	info, _ := o.GetServiceInfo("redis")
	if info.State != StateStopped {
		t.Errorf("redis state = %s, want stopped untouched", info.State)
	}

	o.OnContainerGone("nonexistent") // must not panic
}

func TestOnContainerSeen_UnknownServiceIgnored(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	// Should not panic or create a new entry.
	o.OnContainerSeen("unknown-service", true)

	if _, ok := o.GetServiceInfo("unknown-service"); ok {
		t.Error("OnContainerSeen created entry for unconfigured service")
	}
}
