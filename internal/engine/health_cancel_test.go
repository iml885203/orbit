package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

// Phase 2: a Stop issued while the health check is still polling must cancel
// the health check goroutine via the per-service ctx. Before the fix, the
// health check ran against the daemon's root ctx and kept spinning after
// stop, which is the window that leaks child processes.

func TestHealthCheck_StopsWhenServiceCancelled(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{"api": {Name: "api"}},
	}
	o := testOrchestrator(cfg)

	// Capture the ctx the orchestrator passes to OnHealthCheck and simulate
	// a long-running poll by blocking on ctx.Done.
	hcCtxCh := make(chan context.Context, 1)
	done := make(chan struct{})
	o.OnHealthCheck = func(ctx context.Context, _ string, _ int) error {
		hcCtxCh <- ctx
		go func() {
			<-ctx.Done()
			close(done)
		}()
		return nil
	}

	if err := o.startService(context.Background(), "api"); err != nil {
		t.Fatalf("startService: %v", err)
	}

	// Wait for the health check to have received a ctx.
	var hcCtx context.Context
	select {
	case hcCtx = <-hcCtxCh:
	case <-time.After(time.Second):
		t.Fatal("OnHealthCheck was not invoked")
	}

	// Stop the service. The ctx observed inside OnHealthCheck must be
	// cancelled — proving the orchestrator scoped the health check to the
	// service lifecycle rather than to the root ctx.
	if err := o.StopService(context.Background(), "api"); err != nil {
		t.Fatalf("StopService: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("health check goroutine did not observe cancellation after stop")
	}
	if hcCtx.Err() == nil {
		t.Error("expected health check ctx to be cancelled")
	}
}

func TestHealthCheck_RootCtxStillLive_AfterServiceStopped(t *testing.T) {
	// The service-scoped ctx must be derived from the root ctx but the root
	// ctx must NOT be cancelled by a single service stop. Otherwise other
	// services' health checks would be collaterally killed.
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"api":   {Name: "api"},
			"other": {Name: "other"},
		},
	}
	o := testOrchestrator(cfg)

	var mu sync.Mutex
	ctxs := make(map[string]context.Context)
	hcCalls := make(chan string, 2)
	o.OnHealthCheck = func(ctx context.Context, name string, _ int) error {
		mu.Lock()
		ctxs[name] = ctx
		mu.Unlock()
		hcCalls <- name
		return nil
	}

	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	if err := o.startService(root, "api"); err != nil {
		t.Fatalf("startService api: %v", err)
	}
	if err := o.startService(root, "other"); err != nil {
		t.Fatalf("startService other: %v", err)
	}
	// Drain both HC invocations.
	for i := 0; i < 2; i++ {
		select {
		case <-hcCalls:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for OnHealthCheck")
		}
	}

	if err := o.StopService(context.Background(), "api"); err != nil {
		t.Fatalf("StopService: %v", err)
	}

	mu.Lock()
	apiCtx := ctxs["api"]
	otherCtx := ctxs["other"]
	mu.Unlock()
	if apiCtx.Err() == nil {
		t.Error("api ctx should be cancelled after its stop")
	}
	if err := otherCtx.Err(); err != nil {
		t.Errorf("other ctx should remain live, got %v", err)
	}
	if err := root.Err(); err != nil {
		t.Errorf("root ctx should remain live, got %v", err)
	}
}
