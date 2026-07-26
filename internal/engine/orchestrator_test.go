package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

// testOrchestrator creates an orchestrator with noop callbacks for testing.
func testOrchestrator(cfg *config.Config) *Orchestrator {
	o := NewOrchestrator(config.NewHolder(cfg), nil, nil)
	o.OnStartContainer = func(_ context.Context, _ string, _ *config.Container) error { return nil }
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

// Note: "SkipsDisabled" and "IgnoresDisabledDeps" tests were removed — the
// enabled-filter concept no longer exists; full enumeration registers every
// service in cfg. Coverage of the new behavior lives in
// orchestrator_full_enum_test.go.

func TestNotifyDependents(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis"},
		},
		Services: map[string]*config.Service{
			"api":      {Name: "api", DependsOn: []string{"redis"}},
			"frontend": {Name: "frontend", DependsOn: []string{"api"}},
		},
	}
	o := testOrchestrator(cfg)
	o.Start([]string{"redis", "api", "frontend"})
	// Drain DepsReady event for redis (no deps), so later assertions see the api event.
	select {
	case <-o.events:
	case <-time.After(time.Second):
		t.Fatal("timeout draining initial DepsReady for redis")
	}

	// Simulate redis becoming healthy
	o.mu.Lock()
	o.services["redis"].State = StateHealthy
	o.mu.Unlock()
	o.notifyDependents("redis")

	// api's PendingDeps should now be empty
	api, _ := o.GetServiceInfo("api")
	if len(api.PendingDeps) != 0 {
		t.Errorf("api PendingDeps after redis healthy = %v, want empty", api.PendingDeps)
	}

	// frontend still depends on api (not healthy yet)
	frontend, _ := o.GetServiceInfo("frontend")
	if len(frontend.PendingDeps) != 1 {
		t.Errorf("frontend PendingDeps = %v, want {api: true}", frontend.PendingDeps)
	}

	// Drain the DepsReady event for api
	select {
	case evt := <-o.events:
		if evt.Type != EventDepsReady || evt.Service != "api" {
			t.Errorf("expected DepsReady for api, got %s for %s", evt.Type, evt.Service)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for DepsReady event")
	}
}

func TestNotifyDependents_MultipleDeps(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis"},
			"sql":   {Name: "sql"},
		},
		Services: map[string]*config.Service{
			"api": {Name: "api", DependsOn: []string{"redis", "sql"}},
		},
	}
	o := testOrchestrator(cfg)
	o.Start([]string{"redis", "sql", "api"})
	// Drain initial DepsReady for redis and sql (no deps).
	for i := 0; i < 2; i++ {
		select {
		case <-o.events:
		case <-time.After(time.Second):
			t.Fatal("timeout draining initial DepsReady events")
		}
	}

	// Only redis healthy — api should still have sql pending
	o.mu.Lock()
	o.services["redis"].State = StateHealthy
	o.mu.Unlock()
	o.notifyDependents("redis")

	api, _ := o.GetServiceInfo("api")
	if len(api.PendingDeps) != 1 || !api.PendingDeps["sql"] {
		t.Errorf("api PendingDeps after redis = %v, want {sql: true}", api.PendingDeps)
	}

	// Now sql healthy — api should have no pending deps
	o.mu.Lock()
	o.services["sql"].State = StateHealthy
	o.mu.Unlock()
	o.notifyDependents("sql")

	api, _ = o.GetServiceInfo("api")
	if len(api.PendingDeps) != 0 {
		t.Errorf("api PendingDeps after both = %v, want empty", api.PendingDeps)
	}

	select {
	case evt := <-o.events:
		if evt.Type != EventDepsReady || evt.Service != "api" {
			t.Errorf("expected DepsReady for api, got %s for %s", evt.Type, evt.Service)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for DepsReady event")
	}
}

func TestHandleEvent_HealthOK(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis"},
		},
		Services: map[string]*config.Service{},
	}
	o := testOrchestrator(cfg)

	o.mu.Lock()
	o.services["redis"].State = StateStarting
	o.mu.Unlock()

	ctx := context.Background()
	_ = o.handleEvent(ctx, Event{Type: EventHealthOK, Service: "redis"})

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateHealthy {
		t.Errorf("state = %s, want healthy", info.State)
	}
	if info.HealthyAt.IsZero() {
		t.Error("HealthyAt should be set")
	}
}

func TestHandleEvent_HealthFail(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{},
		Services: map[string]*config.Service{
			"api": {Name: "api"},
		},
	}
	o := testOrchestrator(cfg)

	o.mu.Lock()
	o.services["api"].State = StateStarting
	o.mu.Unlock()

	ctx := context.Background()
	_ = o.handleEvent(ctx, Event{Type: EventHealthFail, Service: "api"})

	info, _ := o.GetServiceInfo("api")
	if info.State != StateDegraded {
		t.Errorf("state = %s, want degraded", info.State)
	}
}

func TestHandleEvent_ProcessExited(t *testing.T) {
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

	ctx := context.Background()
	_ = o.handleEvent(ctx, Event{Type: EventProcessExited, Service: "api", Message: "exited: exit status 2"})

	info, _ := o.GetServiceInfo("api")
	if info.State != StateDegraded {
		t.Errorf("state = %s, want degraded (unexpected exit is a crash, not a stop)", info.State)
	}
	if info.StateReason != "exited: exit status 2" {
		t.Errorf("state reason = %q, want the exit message", info.StateReason)
	}
}

func TestHandleEvent_ProcessExited_ReasonClearedOnRecovery(t *testing.T) {
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

	ctx := context.Background()
	_ = o.handleEvent(ctx, Event{Type: EventProcessExited, Service: "api", Message: "exited"})
	// Recovery (e.g. restart brings it back) must not carry a stale reason.
	_ = o.handleEvent(ctx, Event{Type: EventHealthOK, Service: "api"})

	info, _ := o.GetServiceInfo("api")
	if info.State != StateHealthy {
		t.Fatalf("state = %s, want healthy", info.State)
	}
	if info.StateReason != "" {
		t.Errorf("state reason = %q, want cleared on recovery", info.StateReason)
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
	case <-time.After(100 * time.Millisecond):
		// OK — no event expected
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
	case <-time.After(100 * time.Millisecond):
		// OK
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
	err := o.startService(ctx, "api")
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
	_ = o.startService(ctx, "api")

	time.Sleep(50 * time.Millisecond)
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

func TestRestartService_NarratesProgressAndStopFailure(t *testing.T) {
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

	if err := o.RestartService(context.Background(), "sql-server"); err != nil {
		t.Fatalf("RestartService: %v", err)
	}

	joined := strings.Join(narration, "\n")
	if !strings.Contains(joined, "restart requested") {
		t.Errorf("expected 'restart requested' narration, got:\n%s", joined)
	}
	if !strings.Contains(joined, "stop failed: docker API exploded") {
		t.Errorf("expected stop-failed narration, got:\n%s", joined)
	}
}

func TestCalcPendingDeps_SkipsDetached(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"frontend": {Name: "frontend", DependsOn: []string{"api", "redis"}},
			"api": {Name: "api"},
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
