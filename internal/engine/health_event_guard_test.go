package engine

import (
	"context"
	"testing"

	"github.com/iml885203/orbit/config"
)

func singleServiceOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()
	cfg := &config.Config{
		Services: map[string]*config.Service{"api": {Name: "api"}},
	}
	return testOrchestrator(cfg)
}

// setServiceState force-sets state and generation for guard tests.
func setServiceState(o *Orchestrator, name string, state ServiceState, generation int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	info := o.services[name]
	info.State = state
	info.Generation = generation
}

// A health success from a previous start (cancelled, but with the event
// already in flight) must not touch the current generation's state.
func TestHealthOK_StaleGenerationIgnored(t *testing.T) {
	o := singleServiceOrchestrator(t)
	setServiceState(o, "api", StateStarting, 2)

	_ = o.handleEvent(context.Background(), Event{Type: EventHealthOK, Service: "api", Generation: 1})

	info, _ := o.GetServiceInfo("api")
	if info.State != StateStarting {
		t.Fatalf("stale HealthOK flipped state to %s, want starting", info.State)
	}
}

// A recovery success racing StopService must not resurrect the service.
func TestHealthOK_StoppedStatesNotResurrected(t *testing.T) {
	for _, state := range []ServiceState{StateStopping, StateStopped, StatePending, StateRestarting} {
		o := singleServiceOrchestrator(t)
		setServiceState(o, "api", state, 1)

		_ = o.handleEvent(context.Background(), Event{Type: EventHealthOK, Service: "api", Generation: 1})

		info, _ := o.GetServiceInfo("api")
		if info.State != state {
			t.Fatalf("HealthOK resurrected %s to %s", state, info.State)
		}
	}
}

// Recovery flipping a degraded service back to healthy is the feature —
// same generation, degraded state, must transition.
func TestHealthOK_DegradedSameGenerationRecovers(t *testing.T) {
	o := singleServiceOrchestrator(t)
	setServiceState(o, "api", StateDegraded, 3)

	_ = o.handleEvent(context.Background(), Event{Type: EventHealthOK, Service: "api", Generation: 3})

	info, _ := o.GetServiceInfo("api")
	if info.State != StateHealthy {
		t.Fatalf("same-generation HealthOK on degraded = %s, want healthy", info.State)
	}
}

// Generation-0 events belong to adopted (never-started) services only:
// they match a gen-0 service, and are dropped once the service has been
// (re)started — this is what keeps a daemon-reconnected process's late
// poll-detected exit from degrading or cancelling a successor generation.
func TestGenerationZeroEvents_MatchOnlyAdoptedServices(t *testing.T) {
	o := singleServiceOrchestrator(t)

	// Adopted service (gen 0): its own unversioned events work.
	setServiceState(o, "api", StateStarting, 0)
	_ = o.handleEvent(context.Background(), Event{Type: EventHealthOK, Service: "api"})
	info, _ := o.GetServiceInfo("api")
	if info.State != StateHealthy {
		t.Fatalf("gen-0 HealthOK on adopted service = %s, want healthy", info.State)
	}

	// After a restart (gen 5), leftover gen-0 events are stale.
	setServiceState(o, "api", StateStarting, 5)
	_ = o.handleEvent(context.Background(), Event{Type: EventProcessExited, Service: "api", Message: "late poll exit"})
	info, _ = o.GetServiceInfo("api")
	if info.State != StateStarting {
		t.Fatalf("gen-0 exit degraded a restarted service: %s", info.State)
	}
	_ = o.handleEvent(context.Background(), Event{Type: EventHealthOK, Service: "api"})
	info, _ = o.GetServiceInfo("api")
	if info.State != StateStarting {
		t.Fatalf("gen-0 HealthOK flipped a restarted service: %s", info.State)
	}
}

// A crashed process cancels the per-service lifecycle ctx so startup polls
// and recovery loops stop probing a port nothing will reopen — otherwise a
// dead service keeps looking "recovering" until someone restarts it.
func TestProcessExited_CancelsLifecycleCtx(t *testing.T) {
	o := singleServiceOrchestrator(t)
	o.mu.Lock()
	info := o.services["api"]
	info.State = StateHealthy
	ctx, cancel := context.WithCancel(context.Background())
	info.ctx = ctx
	info.cancel = cancel
	o.mu.Unlock()

	_ = o.handleEvent(context.Background(), Event{Type: EventProcessExited, Service: "api", Message: "exited: boom"})

	if ctx.Err() == nil {
		t.Fatal("expected lifecycle ctx cancelled after process exit")
	}
	got, _ := o.GetServiceInfo("api")
	if got.State != StateDegraded || got.StateReason != "exited: boom" {
		t.Fatalf("got %s/%q, want degraded/exited: boom", got.State, got.StateReason)
	}
}

func TestProcessExitEvidenceSurvivesCancellationNoise(t *testing.T) {
	o := singleServiceOrchestrator(t)
	setServiceState(o, "api", StateHealthy, 2)

	_ = o.handleEvent(context.Background(), Event{
		Type:       EventProcessExited,
		Service:    "api",
		Message:    "exited: exit status 1",
		Evidence:   "ModuleNotFoundError: No module named 'humanize'",
		Generation: 2,
	})
	_ = o.handleEvent(context.Background(), Event{
		Type:       EventHealthFail,
		Service:    "api",
		Message:    "context canceled",
		Err:        context.Canceled,
		Generation: 2,
	})

	info, _ := o.GetServiceInfo("api")
	if info.StateReason != "exited: exit status 1" {
		t.Fatalf("state reason = %q", info.StateReason)
	}
	if info.FailureEvidence != "ModuleNotFoundError: No module named 'humanize'" {
		t.Fatalf("failure evidence = %q", info.FailureEvidence)
	}
}

// A late exit notification from a replaced process must not degrade — or
// (worse) cancel the lifecycle of — the successor generation.
func TestProcessExited_StaleGenerationIgnored(t *testing.T) {
	o := singleServiceOrchestrator(t)
	o.mu.Lock()
	info := o.services["api"]
	info.State = StateStarting
	info.Generation = 2
	ctx, cancel := context.WithCancel(context.Background())
	info.ctx = ctx
	info.cancel = cancel
	o.mu.Unlock()

	_ = o.handleEvent(context.Background(), Event{Type: EventProcessExited, Service: "api", Message: "exited", Generation: 1})

	got, _ := o.GetServiceInfo("api")
	if got.State != StateStarting {
		t.Fatalf("stale exit degraded the new generation: %s", got.State)
	}
	if ctx.Err() != nil {
		t.Fatal("stale exit cancelled the new generation's lifecycle ctx")
	}
}

// A stale-generation failure must not degrade the new generation, and a
// same-generation failure while already degraded refreshes the reason
// (zombie diagnosis during recovery) without a state change.
func TestHealthFail_GenerationAndReasonSemantics(t *testing.T) {
	o := singleServiceOrchestrator(t)
	setServiceState(o, "api", StateHealthy, 2)

	_ = o.handleEvent(context.Background(), Event{Type: EventHealthFail, Service: "api", Message: "old probe", Generation: 1})
	info, _ := o.GetServiceInfo("api")
	if info.State != StateHealthy {
		t.Fatalf("stale HealthFail degraded the new generation: %s", info.State)
	}

	_ = o.handleEvent(context.Background(), Event{Type: EventHealthFail, Service: "api", Message: "budget spent", Generation: 2})
	info, _ = o.GetServiceInfo("api")
	if info.State != StateDegraded || info.StateReason != "budget spent" {
		t.Fatalf("got %s/%q, want degraded/budget spent", info.State, info.StateReason)
	}

	_ = o.handleEvent(context.Background(), Event{Type: EventHealthFail, Service: "api", Message: "zombie detected", Generation: 2})
	info, _ = o.GetServiceInfo("api")
	if info.State != StateDegraded || info.StateReason != "zombie detected" {
		t.Fatalf("got %s/%q, want degraded with refreshed reason", info.State, info.StateReason)
	}
}
