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
	o.services["redis"].AwaitingContainerRemoval = true
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

func TestOnContainerGoneKeepsStartupFailureDegraded(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)

	o.mu.Lock()
	o.services["redis"].Transition(StateDegraded)
	o.services["redis"].StateReason = "cannot start redis: port 26379 is already in use"
	o.mu.Unlock()

	o.OnContainerGone("redis")

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateDegraded {
		t.Errorf("redis state = %s, want degraded startup evidence preserved", info.State)
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

func TestOnContainerObserved_ExternalRestartResetsRuntimeTruthOnce(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)
	firstStart := time.Now().Add(-time.Minute)
	secondStart := time.Now().Add(-time.Second)

	if restart := o.OnContainerObserved("redis", true, firstStart); restart != nil {
		t.Fatal("initial observation classified as a restart")
	}
	if restart := o.OnContainerObserved("redis", true, firstStart); restart != nil {
		t.Fatal("unchanged runtime classified as a restart")
	}
	restart := o.OnContainerObserved("redis", true, secondStart)
	if restart == nil || restart.Name != "redis" || !restart.StartedAt.Equal(secondStart) {
		t.Fatalf("restart = %#v, want redis at second Docker start", restart)
	}
	if duplicate := o.OnContainerObserved("redis", true, secondStart); duplicate != nil {
		t.Fatal("ordinary poll incremented external restart twice")
	}

	info, _ := o.GetServiceInfo("redis")
	if info.ExternalRestartCount != 1 {
		t.Fatalf("ExternalRestartCount = %d, want 1", info.ExternalRestartCount)
	}
	if !info.ContainerStartedAt.Equal(secondStart) {
		t.Fatalf("ContainerStartedAt = %s, want %s", info.ContainerStartedAt, secondStart)
	}
}

func TestOnContainerObserved_ManagedStartIsNotExternal(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)
	firstStart := time.Now().Add(-time.Minute)
	secondStart := time.Now()
	o.OnContainerObserved("redis", true, firstStart)

	o.mu.Lock()
	o.services["redis"].ExpectingContainerStart = true
	o.mu.Unlock()
	if restart := o.OnContainerObserved("redis", true, secondStart); restart != nil {
		t.Fatalf("managed replacement classified as external: %#v", restart)
	}
	info, _ := o.GetServiceInfo("redis")
	if info.ExternalRestartCount != 0 {
		t.Fatalf("ExternalRestartCount = %d, want 0", info.ExternalRestartCount)
	}
}

func TestRestoreContainerRuntimeDetectsRestartWhileDaemonWasDown(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)
	firstStart := time.Now().Add(-time.Minute)
	secondStart := time.Now()
	lastRestart := time.Now().Add(-time.Hour)
	o.RestoreContainerRuntime("redis", firstStart, 2, lastRestart, firstStart)

	restart := o.OnContainerObserved("redis", true, secondStart)
	if restart == nil {
		t.Fatal("container restart while daemon was down was not detected")
	}
	info, _ := o.GetServiceInfo("redis")
	if info.ExternalRestartCount != 3 {
		t.Fatalf("ExternalRestartCount = %d, want 3", info.ExternalRestartCount)
	}
}

func TestOnContainerObserved_ManagedStartDoesNotRewriteLastExternalEvent(t *testing.T) {
	cfg := twoContainerCfg()
	o := newTestOrchestrator(cfg)
	initialStart := time.Now().Add(-time.Minute)
	externalStart := time.Now().Add(-time.Second)
	managedStart := time.Now()
	o.OnContainerObserved("redis", true, initialStart)
	o.OnContainerObserved("redis", true, externalStart)

	o.mu.Lock()
	o.services["redis"].ExpectingContainerStart = true
	o.mu.Unlock()
	o.OnContainerObserved("redis", true, managedStart)

	info, _ := o.GetServiceInfo("redis")
	if info.ExternalRestartCount != 1 {
		t.Fatalf("ExternalRestartCount = %d, want 1", info.ExternalRestartCount)
	}
	if !info.LastExternalStartedAt.Equal(externalStart) {
		t.Fatalf("LastExternalStartedAt = %s, want immutable %s", info.LastExternalStartedAt, externalStart)
	}
	if !info.ContainerStartedAt.Equal(managedStart) {
		t.Fatalf("ContainerStartedAt = %s, want current managed runtime %s", info.ContainerStartedAt, managedStart)
	}
}
