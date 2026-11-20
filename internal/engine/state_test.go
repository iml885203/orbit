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
	first = time.Unix(1000, 0)
	info.HealthyAt = first
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
