package engine

import (
	"context"
	"testing"
	"time"
)

func TestServiceInfo_TransitionStartingResetsHealthyAtAndStampsStartedAt(t *testing.T) {
	info := &ServiceInfo{
		HealthyAt: time.Unix(1000, 0), // pretend it was healthy in a previous run
		StartedAt: time.Time{},
	}
	before := time.Now()
	info.Transition(StateStarting)
	after := time.Now()

	if info.State != StateStarting {
		t.Errorf("State = %v, want StateStarting", info.State)
	}
	if !info.HealthyAt.IsZero() {
		t.Errorf("HealthyAt should be reset on Starting, got %v", info.HealthyAt)
	}
	if info.StartedAt.Before(before) || info.StartedAt.After(after) {
		t.Errorf("StartedAt should be set near now, got %v (range %v..%v)", info.StartedAt, before, after)
	}
}

func TestServiceInfo_TransitionHealthyStampsHealthyAtOnceOnly(t *testing.T) {
	info := &ServiceInfo{}
	info.Transition(StateHealthy)
	first := info.HealthyAt
	if first.IsZero() {
		t.Fatalf("HealthyAt should be set on first Healthy")
	}
	// A second Healthy transition (e.g., after drift recovery) should
	// preserve the original timestamp so uptime stays continuous.
	time.Sleep(time.Millisecond)
	info.Transition(StateHealthy)
	if !info.HealthyAt.Equal(first) {
		t.Errorf("HealthyAt should not be overwritten on subsequent Healthy, got %v want %v", info.HealthyAt, first)
	}
}

func TestServiceInfo_TransitionStoppingCancelsLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	info := &ServiceInfo{ctx: ctx, cancel: cancel}
	info.Transition(StateStopping)
	select {
	case <-ctx.Done():
		// ok
	default:
		t.Errorf("ctx should be cancelled after Transition(StateStopping)")
	}
}

func TestServiceInfo_TransitionStoppedAlsoCancelsLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	info := &ServiceInfo{ctx: ctx, cancel: cancel}
	info.Transition(StateStopped)
	select {
	case <-ctx.Done():
		// ok
	default:
		t.Errorf("ctx should be cancelled after Transition(StateStopped)")
	}
}

func TestServiceInfo_MarkDependencyReadyRemovesDepAndReportsCompletion(t *testing.T) {
	info := &ServiceInfo{
		PendingDeps: map[string]bool{"redis": true, "sql-server": true},
	}
	if becameReady := info.MarkDependencyReady("redis"); becameReady {
		t.Errorf("becameReady=true while sql-server still pending")
	}
	if _, still := info.PendingDeps["redis"]; still {
		t.Errorf("redis should have been removed from PendingDeps")
	}
	if becameReady := info.MarkDependencyReady("sql-server"); !becameReady {
		t.Errorf("becameReady=false after final dep cleared")
	}
}

func TestServiceInfo_MarkDependencyReadyIgnoresUnknownDep(t *testing.T) {
	info := &ServiceInfo{PendingDeps: map[string]bool{"redis": true}}
	if becameReady := info.MarkDependencyReady("not-a-dep"); becameReady {
		t.Errorf("becameReady=true for a dep that was never pending")
	}
	if _, still := info.PendingDeps["redis"]; !still {
		t.Errorf("unrelated dep should be untouched")
	}
}

func TestServiceInfo_TransitionDoesNotValidate(t *testing.T) {
	// The method is advisory only: it does not reject illegal transitions.
	// This is intentional — validation belongs in orchestrator decisions,
	// not in the data type — but we lock it in a test so future readers
	// know the behaviour was a choice, not an oversight.
	info := &ServiceInfo{State: StateStopped}
	info.Transition(StateHealthy)
	if info.State != StateHealthy {
		t.Errorf("Transition should not refuse %v→%v, got State=%v", StateStopped, StateHealthy, info.State)
	}
}
