package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
)

func startQueuedService(ctx context.Context, o *Orchestrator, name string) error {
	o.Start([]string{name})
	select {
	case evt := <-o.events:
		return o.handleEvent(ctx, evt)
	default:
		return nil
	}
}

// testOrchestrator creates an orchestrator with noop callbacks for testing.
func testOrchestrator(cfg *config.Config) *Orchestrator {
	o := NewOrchestrator(config.NewHolder(cfg), nil, nil)
	o.OnStartContainer = func(_ context.Context, _ string, _ int, _ *config.Container) error { return nil }
	o.OnStopContainer = func(_ context.Context, _ string) error { return nil }
	o.OnStartProcess = func(_ context.Context, _ string, _ int, _ *config.Config, _ *config.Service) error { return nil }
	o.OnStopProcess = func(_ string) error { return nil }
	o.OnHealthCheck = func(_ context.Context, _ string, _ int) error { return nil }
	return o
}

func TestNewOrchestrator_PendingDeps(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis"},
		},
		Services: map[string]*config.Service{
			"api": {Name: "api", DependsOn: []string{"redis"}},
		},
	}

	o := NewOrchestrator(config.NewHolder(cfg), nil, nil)
	// Full-enum mode computes PendingDeps when Start is called.
	o.Start([]string{"redis", "api"})

	redis, ok := o.GetServiceInfo("redis")
	if !ok {
		t.Fatal("redis not found")
	}
	if len(redis.PendingDeps) != 0 {
		t.Errorf("redis PendingDeps = %d, want 0", len(redis.PendingDeps))
	}
	if redis.Kind != "container" {
		t.Errorf("redis Kind = %q, want container", redis.Kind)
	}

	api, ok := o.GetServiceInfo("api")
	if !ok {
		t.Fatal("api not found")
	}
	if len(api.PendingDeps) != 1 || !api.PendingDeps["redis"] {
		t.Errorf("api PendingDeps = %v, want {redis: true}", api.PendingDeps)
	}
	if api.Kind != "service" {
		t.Errorf("api Kind = %q, want service", api.Kind)
	}
}

func TestRunningOrchestratorStartsDependentsOnlyAfterAllDependenciesAreHealthy(t *testing.T) {
	for _, dependencies := range [][]string{{"redis"}, {"redis", "sql"}} {
		t.Run(strings.Join(dependencies, "+"), func(t *testing.T) {
			cfg := &config.Config{Containers: map[string]*config.Container{}, Services: map[string]*config.Service{
				"api": {Name: "api", DependsOn: dependencies}, "frontend": {Name: "frontend", DependsOn: []string{"api"}},
			}}
			for _, name := range dependencies {
				cfg.Containers[name] = &config.Container{Name: name}
			}
			o := testOrchestrator(cfg)
			runOrchestratorForTest(t, o)
			names := append(append([]string{}, dependencies...), "api", "frontend")
			o.Start(names)
			for _, name := range dependencies {
				waitForOrchestratorState(t, o, name, StateStarting)
			}
			for i, name := range dependencies {
				if info, _ := o.GetServiceInfo("api"); info.State != StatePending {
					t.Fatalf("api unblocked before dependency %s: %+v", name, info)
				}
				info, _ := o.GetServiceInfo(name)
				o.Events() <- Event{Type: EventHealthOK, Service: name, Generation: info.Generation}
				waitForOrchestratorState(t, o, name, StateHealthy)
				if i < len(dependencies)-1 {
					waitForPendingDependencies(t, o, "api", dependencies[i+1:])
					if info, _ := o.GetServiceInfo("api"); info.State != StatePending {
						t.Fatalf("api started before all dependencies: %+v", info)
					}
				}
			}
			api := waitForOrchestratorState(t, o, "api", StateStarting)
			if info, _ := o.GetServiceInfo("frontend"); info.State != StatePending {
				t.Fatalf("frontend bypassed api readiness: %+v", info)
			}
			o.Events() <- Event{Type: EventHealthOK, Service: "api", Generation: api.Generation}
			waitForOrchestratorState(t, o, "frontend", StateStarting)
		})
	}
}

func runOrchestratorForTest(t *testing.T, o *Orchestrator) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); _ = o.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("orchestrator event loop did not exit")
		}
	})
}

func waitForOrchestratorState(t *testing.T, o *Orchestrator, name string, state ServiceState) ServiceInfo {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		info, ok := o.GetServiceInfo(name)
		if ok && info.State == state {
			return info
		}
		select {
		case <-deadline.C:
			t.Fatalf("%s state = %s, want %s", name, info.State, state)
		case <-ticker.C:
		}
	}
}

func waitForPendingDependencies(t *testing.T, o *Orchestrator, name string, want []string) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		// GetServiceInfo returns a shallow snapshot; copy the pending map under its owner's lock.
		o.mu.RLock()
		pending := make([]string, 0, len(o.services[name].PendingDeps))
		for dependency := range o.services[name].PendingDeps {
			pending = append(pending, dependency)
		}
		o.mu.RUnlock()
		slices.Sort(pending)
		if slices.Equal(pending, want) {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("%s pending = %v, want %v", name, pending, want)
		case <-ticker.C:
		}
	}
}

func TestRunningOrchestratorHealthFailureCrashAndRestart(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api"}}}
	o := testOrchestrator(cfg)
	runOrchestratorForTest(t, o)
	o.Start([]string{"api"})
	started := waitForOrchestratorState(t, o, "api", StateStarting)
	if started.StartedAt.IsZero() || !started.HealthyAt.IsZero() {
		t.Fatalf("startup timestamps: %+v", started)
	}
	o.Events() <- Event{Type: EventHealthFail, Service: "api", Generation: started.Generation, Message: "HTTP 500", FailureKind: FailureKindHealth}
	failed := waitForOrchestratorState(t, o, "api", StateDegraded)
	if failed.StateReason != "HTTP 500" || failed.FailureKind != FailureKindHealth {
		t.Fatalf("probe evidence lost: %+v", failed)
	}
	o.Events() <- Event{Type: EventHealthOK, Service: "api", Generation: started.Generation}
	healthy := waitForOrchestratorState(t, o, "api", StateHealthy)
	if healthy.HealthyAt.IsZero() || healthy.StateReason != "" {
		t.Fatalf("recovered health: %+v", healthy)
	}
	o.Events() <- Event{Type: EventProcessExited, Service: "api", Generation: started.Generation, Message: "exited: exit status 2", Evidence: "application failed"}
	crashed := waitForOrchestratorState(t, o, "api", StateDegraded)
	if crashed.StateReason != "exited: exit status 2" || crashed.FailureEvidence != "application failed" {
		t.Fatalf("crash evidence lost: %+v", crashed)
	}
	if err := o.RestartService(t.Context(), "api"); err != nil {
		t.Fatal(err)
	}
	restarted := waitForOrchestratorState(t, o, "api", StateStarting)
	if restarted.Generation <= started.Generation || !restarted.HealthyAt.IsZero() {
		t.Fatalf("restart retained prior lifecycle: %+v", restarted)
	}
	o.Events() <- Event{Type: EventHealthOK, Service: "api", Generation: restarted.Generation}
	recovered := waitForOrchestratorState(t, o, "api", StateHealthy)
	if recovered.StateReason != "" || recovered.FailureEvidence != "" {
		t.Fatalf("stale failure after restart: %+v", recovered)
	}
}

func TestHandleEvent_ProcessExited_IgnoresDuringStopping(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{},
		Services: map[string]*config.Service{
			"api": {Name: "api"},
		},
	}
	o := testOrchestrator(cfg)

	o.mu.Lock()
	o.services["api"].State = StateStopping
	o.mu.Unlock()

	ctx := context.Background()
	_ = o.handleEvent(ctx, Event{Type: EventProcessExited, Service: "api"})

	info, _ := o.GetServiceInfo("api")
	if info.State != StateStopping {
		t.Errorf("state = %s, want stopping (should not change)", info.State)
	}
}

func TestHandleEvent_BuildLifecycle(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{},
		Services: map[string]*config.Service{
			"api": {Name: "api"},
		},
	}
	o := testOrchestrator(cfg)
	ctx := context.Background()
	setServiceState(o, "api", StateStarting, 0)

	_ = o.handleEvent(ctx, Event{Type: EventBuildStarted, Service: "api"})
	info, _ := o.GetServiceInfo("api")
	if info.State != StateBuilding {
		t.Errorf("after BuildStarted: state = %s, want building", info.State)
	}

	_ = o.handleEvent(ctx, Event{Type: EventBuildComplete, Service: "api"})
	info, _ = o.GetServiceInfo("api")
	if info.State != StateStarting {
		t.Errorf("after BuildComplete: state = %s, want starting", info.State)
	}
}

func TestHandleEvent_BuildFailed(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{},
		Services: map[string]*config.Service{
			"api": {Name: "api"},
		},
	}
	o := testOrchestrator(cfg)
	ctx := context.Background()
	setServiceState(o, "api", StateStarting, 0)

	_ = o.handleEvent(ctx, Event{Type: EventBuildStarted, Service: "api"})
	_ = o.handleEvent(ctx, Event{Type: EventBuildFailed, Service: "api"})

	info, _ := o.GetServiceInfo("api")
	if info.State != StateDegraded {
		t.Errorf("after BuildFailed: state = %s, want degraded", info.State)
	}
}

func TestStart_NewService(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis"},
		},
		Services: map[string]*config.Service{
			"api": {Name: "api", DependsOn: []string{"redis"}},
		},
	}
	o := testOrchestrator(cfg)

	// Mark redis healthy
	o.mu.Lock()
	o.services["redis"].State = StateHealthy
	o.mu.Unlock()

	// Start api
	o.Start([]string{"api"})

	api, ok := o.GetServiceInfo("api")
	if !ok {
		t.Fatal("api not found after Start")
	}
	if api.State != StatePending {
		t.Errorf("api state = %s, want pending", api.State)
	}
	// redis is already healthy, so api should have no pending deps
	if len(api.PendingDeps) != 0 {
		t.Errorf("api PendingDeps = %v, want empty (redis is healthy)", api.PendingDeps)
	}

	// Should emit DepsReady
	select {
	case evt := <-o.events:
		if evt.Type != EventDepsReady || evt.Service != "api" {
			t.Errorf("expected DepsReady for api, got %s for %s", evt.Type, evt.Service)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for DepsReady")
	}
}

func TestStart_RequeueStopped(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{},
		Services: map[string]*config.Service{
			"api": {Name: "api"},
		},
	}
	o := testOrchestrator(cfg)

	// api starts in StateStopped by default under full-enum mode.
	api, _ := o.GetServiceInfo("api")
	if api.State != StateStopped {
		t.Fatalf("precondition: api state = %s, want stopped", api.State)
	}

	o.Start([]string{"api"})

	api, _ = o.GetServiceInfo("api")
	if api.State != StatePending {
		t.Errorf("api state = %s, want pending (should be re-queued)", api.State)
	}

	// Should emit DepsReady (no deps)
	select {
	case evt := <-o.events:
		if evt.Type != EventDepsReady || evt.Service != "api" {
			t.Errorf("expected DepsReady for api, got %s for %s", evt.Type, evt.Service)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for DepsReady")
	}
}

func TestStart_SkipHealthy(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{},
		Services: map[string]*config.Service{
			"api": {Name: "api"},
		},
	}
	o := testOrchestrator(cfg)

	// Set api to healthy
	o.mu.Lock()
	o.services["api"].State = StateHealthy
	o.mu.Unlock()

	o.Start([]string{"api"})

	api, _ := o.GetServiceInfo("api")
	if api.State != StateHealthy {
		t.Errorf("api state = %s, want healthy (should be skipped)", api.State)
	}

	// Should NOT emit any event
	select {
	case evt := <-o.events:
		t.Errorf("unexpected event: %s for %s", evt.Type, evt.Service)
	default:
	}
}

func TestStart_WithUnhealthyDep(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis"},
		},
		Services: map[string]*config.Service{
			"api": {Name: "api", DependsOn: []string{"redis"}},
		},
	}
	o := testOrchestrator(cfg)

	// redis is stopped (not healthy); starting api should leave it pending on redis.
	o.Start([]string{"api"})

	api, _ := o.GetServiceInfo("api")
	if len(api.PendingDeps) != 1 || !api.PendingDeps["redis"] {
		t.Errorf("api PendingDeps = %v, want {redis: true}", api.PendingDeps)
	}

	// Should NOT emit DepsReady (dep not ready)
	select {
	case evt := <-o.events:
		t.Errorf("unexpected event: %s for %s", evt.Type, evt.Service)
	default:
	}
}

func TestStartService_TransitionsToStarting(t *testing.T) {
	started := make(chan string, 1)
	cfg := &config.Config{
		Containers: map[string]*config.Container{},
		Services: map[string]*config.Service{
			"api": {Name: "api"},
		},
	}
	o := testOrchestrator(cfg)
	o.OnStartProcess = func(_ context.Context, name string, _ int, _ *config.Config, _ *config.Service) error {
		started <- name
		return nil
	}

	ctx := context.Background()
	err := startQueuedService(ctx, o, "api")
	if err != nil {
		t.Fatalf("startService error: %v", err)
	}

	info, _ := o.GetServiceInfo("api")
	if info.State != StateStarting {
		t.Errorf("state = %s, want starting", info.State)
	}

	select {
	case name := <-started:
		if name != "api" {
			t.Errorf("started service = %s, want api", name)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for OnStartProcess callback")
	}
}

func TestStartService_SkipsAlreadyRunning(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{},
		Services: map[string]*config.Service{
			"api": {Name: "api"},
		},
	}
	o := testOrchestrator(cfg)

	o.mu.Lock()
	o.services["api"].State = StateHealthy
	o.mu.Unlock()

	called := false
	o.OnStartProcess = func(_ context.Context, _ string, _ int, _ *config.Config, _ *config.Service) error {
		called = true
		return nil
	}

	ctx := context.Background()
	_ = startQueuedService(ctx, o, "api")

	if called {
		t.Error("OnStartProcess should not be called for already healthy service")
	}
}

func TestOrchestratorSubscribe_ReceivesEvent(t *testing.T) {
	o := testOrchestrator(&config.Config{})
	ch, unsub := o.Subscribe()
	defer unsub()

	o.broadcast(Event{Type: EventHealthOK, Service: "api"})

	select {
	case evt := <-ch:
		if evt.Service != "api" {
			t.Errorf("got service=%q, want api", evt.Service)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestOrchestratorSubscribe_UnsubscribeClosesChannel(t *testing.T) {
	o := testOrchestrator(&config.Config{})
	ch, unsub := o.Subscribe()

	unsub()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed, got value")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for close signal")
	}
}

func TestOrchestratorSubscribe_UnsubscribeIsIdempotent(t *testing.T) {
	o := testOrchestrator(&config.Config{})
	_, unsub := o.Subscribe()
	unsub()
	unsub() // must not panic (double-close)
}

func TestOrchestratorBroadcast_SkipsUnsubscribed(t *testing.T) {
	o := testOrchestrator(&config.Config{})
	_, unsub := o.Subscribe()
	unsub()

	// Should not panic on closed channel — unsubscribe removed it.
	o.broadcast(Event{Type: EventHealthOK, Service: "api"})
}

func TestRestartService_StopFailureDoesNotStartAReplacement(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"sql-server": {Name: "sql-server"},
		},
	}
	o := testOrchestrator(cfg)

	o.OnStopContainer = func(_ context.Context, _ string) error {
		return fmt.Errorf("docker API exploded")
	}

	var narration []string
	o.OnAction = func(_, msg string) { narration = append(narration, msg) }

	err := o.RestartService(context.Background(), "sql-server")
	if err == nil || !strings.Contains(err.Error(), "docker API exploded") {
		t.Fatalf("RestartService error = %v", err)
	}

	joined := strings.Join(narration, "\n")
	if !strings.Contains(joined, "restart requested") {
		t.Errorf("expected 'restart requested' narration, got:\n%s", joined)
	}
	if !strings.Contains(joined, "stop failed: docker API exploded") {
		t.Errorf("expected stop-failed narration, got:\n%s", joined)
	}
	info, _ := o.GetServiceInfo("sql-server")
	if info.State != StateDegraded {
		t.Fatalf("state = %s, want degraded", info.State)
	}
	if info.StateReason != "could not restart: failed to stop existing resource: docker API exploded" {
		t.Fatalf("state reason = %q", info.StateReason)
	}
	select {
	case evt := <-o.events:
		t.Fatalf("replacement was queued after failed stop: %+v", evt)
	default:
	}
}

func TestCalcPendingDeps_SkipsDetached(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"frontend": {Name: "frontend", DependsOn: []string{"api", "redis"}},
			"api":      {Name: "api"},
		},
		Containers: map[string]*config.Container{
			"redis": {Name: "redis"},
		},
	}
	detached := map[string][]string{"frontend": {"api"}}
	o := NewOrchestrator(config.NewHolder(cfg), nil, detached)

	pending := o.calcPendingDeps(cfg, "frontend")
	if pending["api"] {
		t.Errorf("api should be filtered out as detached")
	}
	if !pending["redis"] {
		t.Errorf("redis should remain pending")
	}
}
