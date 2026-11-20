package engine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/engine"
)

type pendingStartProbe struct {
	orchestrator *engine.Orchestrator
	launchDone   chan struct{}
}

func newPendingStartProbe() pendingStartProbe {
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Name: "api", Command: "unused-fake-command"},
	}}
	o := engine.NewOrchestrator(config.NewHolder(cfg), nil, nil)
	done := make(chan struct{}, 8)
	o.OnStartProcess = func(context.Context, string, int, *config.Config, *config.Service) error { return nil }
	o.OnStopProcess = func(string) error { return nil }
	o.OnHealthCheck = func(context.Context, string, int) error {
		done <- struct{}{}
		return nil
	}
	return pendingStartProbe{orchestrator: o, launchDone: done}
}

type pendingStartRunResult struct {
	err   error
	panic any
}

func (p pendingStartProbe) drain(t *testing.T) any {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	events, unsubscribe := p.orchestrator.Subscribe()
	defer unsubscribe()
	const marker = "queued-start-drained"
	p.orchestrator.Events() <- engine.Event{Type: engine.EventShutdown, Message: marker}
	result := make(chan pendingStartRunResult, 1)
	go func() {
		var outcome pendingStartRunResult
		defer func() {
			outcome.panic = recover()
			result <- outcome
		}()
		outcome.err = p.orchestrator.Run(ctx)
	}()
	joined := false
	defer func() {
		cancel()
		if !joined {
			select {
			case <-result:
			case <-time.After(2 * time.Second):
				t.Error("event loop did not join after cancellation")
			}
		}
	}()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case outcome := <-result:
			joined = true
			return outcome.panic
		case event := <-events:
			if event.Type != engine.EventShutdown || event.Message != marker {
				continue
			}
			cancel()
			select {
			case outcome := <-result:
				joined = true
				if outcome.panic != nil {
					return outcome.panic
				}
				if !errors.Is(outcome.err, context.Canceled) {
					t.Fatalf("Run returned %v, want context cancellation", outcome.err)
				}
			case <-deadline.C:
				t.Fatal("event loop did not join after marker")
			}
			// The marker follows every queued ready event. Its broadcast means
			// all earlier handlers have completed their synchronous transitions.
			if info, exists := p.orchestrator.GetServiceInfo("api"); exists && info.State == engine.StateStarting {
				select {
				case <-p.launchDone:
				case <-time.After(2 * time.Second):
					t.Fatal("fake launch did not finish after event-loop cancellation")
				}
			}
			return nil
		case <-deadline.C:
			t.Fatal("event loop did not reach queued marker")
		}
	}
}

func TestQueuedStartReadyAfterStop(t *testing.T) {
	p := newPendingStartProbe()
	p.orchestrator.Start([]string{"api"})
	if err := p.orchestrator.StopService(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	if recovered := p.drain(t); recovered != nil {
		t.Fatalf("queued ready event panicked: %v", recovered)
	}
	info, exists := p.orchestrator.GetServiceInfo("api")
	if !exists || info.State != engine.StateStopped {
		t.Fatalf("queued ready event revived stopped resource: info=%+v exists=%v", info, exists)
	}
}

func TestQueuedStartRestartReadyAfterStop(t *testing.T) {
	p := newPendingStartProbe()
	if err := p.orchestrator.RestartService(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	if err := p.orchestrator.StopService(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	if recovered := p.drain(t); recovered != nil {
		t.Fatalf("queued restart-ready event panicked: %v", recovered)
	}
	info, exists := p.orchestrator.GetServiceInfo("api")
	if !exists || info.State != engine.StateStopped {
		t.Fatalf("queued restart-ready event revived stopped resource: info=%+v exists=%v", info, exists)
	}
}

func TestQueuedStartReadyAfterRemoval(t *testing.T) {
	p := newPendingStartProbe()
	p.orchestrator.Start([]string{"api"})
	if err := p.orchestrator.StopService(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	p.orchestrator.ApplyConfig(&config.Config{}, nil, nil)
	if recovered := p.drain(t); recovered != nil {
		t.Fatalf("queued ready event for removed resource panicked: %v", recovered)
	}
	if _, exists := p.orchestrator.GetServiceInfo("api"); exists {
		t.Fatal("removed resource was recreated by queued ready event")
	}
}

func TestQueuedStartOldReadyCannotBypassNewDependencies(t *testing.T) {
	p := newPendingStartProbe()
	p.orchestrator.Start([]string{"api"})
	if err := p.orchestrator.StopService(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	p.orchestrator.ApplyConfig(&config.Config{Services: map[string]*config.Service{
		"api":        {Name: "api", Command: "unused-fake-command", DependsOn: []string{"dependency"}},
		"dependency": {Name: "dependency", Command: "unused-fake-command"},
	}}, nil, nil)
	p.orchestrator.Start([]string{"api"})
	if recovered := p.drain(t); recovered != nil {
		t.Fatalf("old ready event panicked: %v", recovered)
	}
	info, exists := p.orchestrator.GetServiceInfo("api")
	if !exists || info.State != engine.StatePending || !info.PendingDeps["dependency"] {
		t.Fatalf("old ready event bypassed new pending dependencies: info=%+v exists=%v", info, exists)
	}
}

func TestQueuedStartSurvivesDelayedExitFromStoppedRuntime(t *testing.T) {
	p := newPendingStartProbe()
	p.orchestrator.Start([]string{"api"})
	if recovered := p.drain(t); recovered != nil {
		t.Fatalf("initial launch panicked: %v", recovered)
	}
	previous, _ := p.orchestrator.GetServiceInfo("api")
	if err := p.orchestrator.StopService(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	p.orchestrator.ApplyConfig(&config.Config{Services: map[string]*config.Service{
		"api":        {Name: "api", DependsOn: []string{"dependency"}},
		"dependency": {Name: "dependency"},
	}}, nil, nil)
	p.orchestrator.Start([]string{"api"})
	p.orchestrator.Events() <- engine.Event{
		Type: engine.EventProcessExited, Service: "api", Generation: previous.Generation,
	}
	if recovered := p.drain(t); recovered != nil {
		t.Fatalf("delayed exit panicked: %v", recovered)
	}
	pending, _ := p.orchestrator.GetServiceInfo("api")
	if pending.State != engine.StatePending || !pending.PendingDeps["dependency"] {
		t.Fatalf("delayed old exit invalidated pending replacement: %+v", pending)
	}
	if pending.Generation != previous.Generation {
		t.Fatalf("pending replacement changed runtime generation: previous=%d pending=%d", previous.Generation, pending.Generation)
	}

	p.orchestrator.MarkServiceHealthy("dependency")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.orchestrator.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("replacement event loop did not join after cancellation")
		}
	}()
	select {
	case <-p.launchDone:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement did not launch after current dependency became ready")
	}
	started, _ := p.orchestrator.GetServiceInfo("api")
	if started.State != engine.StateStarting || started.Generation != previous.Generation+1 {
		t.Fatalf("replacement runtime did not start in its next generation: %+v", started)
	}
}
