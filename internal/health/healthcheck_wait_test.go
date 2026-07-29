package health

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

func shortHealth(retries int) *config.HealthCheckConfig {
	return &config.HealthCheckConfig{
		Type:     "healthcheck",
		Interval: time.Millisecond,
		Retries:  retries,
	}
}

func TestWaitForHealthcheck_HealthyImmediately(t *testing.T) {
	insp := &fakeInspector{healthQ: []healthResult{{status: "healthy"}}}
	c := NewChecker(nil, insp)

	if err := c.WaitForHealthy(context.Background(), "svc", shortHealth(5), nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestWaitForHealthcheck_StartingThenHealthy(t *testing.T) {
	insp := &fakeInspector{healthQ: []healthResult{
		{status: "starting"},
		{status: "starting"},
		{status: "healthy"},
	}}
	c := NewChecker(nil, insp)

	if err := c.WaitForHealthy(context.Background(), "svc", shortHealth(10), nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestWaitForHealthcheck_Unhealthy_KeepsRetrying(t *testing.T) {
	// "unhealthy" during startup can be transient; must keep retrying,
	// not fail fast. Retries cap determines the final outcome.
	insp := &fakeInspector{healthQ: []healthResult{
		{status: "unhealthy"},
		{status: "unhealthy"},
		{status: "healthy"},
	}}
	c := NewChecker(nil, insp)

	if err := c.WaitForHealthy(context.Background(), "svc", shortHealth(10), nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestWaitForHealthcheck_RetriesExhausted(t *testing.T) {
	insp := &fakeInspector{healthQ: []healthResult{{status: "starting"}}} // never healthy
	c := NewChecker(nil, insp)

	err := c.WaitForHealthy(context.Background(), "svc", shortHealth(3), nil)
	if err == nil || !strings.Contains(err.Error(), "failed after") {
		t.Fatalf("expected exhaustion error, got %v", err)
	}
}

func TestWaitForHealthcheck_InspectError(t *testing.T) {
	insp := &fakeInspector{healthErr: errors.New("docker down")}
	c := NewChecker(nil, insp)

	err := c.WaitForHealthy(context.Background(), "svc", shortHealth(2), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWaitForHealthcheck_ContextCancel(t *testing.T) {
	insp := &fakeInspector{
		healthQ:      []healthResult{{status: "starting"}},
		healthCalled: make(chan struct{}, 1),
	}
	c := NewChecker(nil, insp)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.WaitForHealthy(ctx, "svc", shortHealth(1000), nil) }()

	select {
	case <-insp.healthCalled:
	case <-time.After(time.Second):
		t.Fatal("health check did not start")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not propagate")
	}
}

func TestWaitForHealthcheck_MissingInspector(t *testing.T) {
	c := NewChecker(nil, nil)
	err := c.WaitForHealthy(context.Background(), "svc", shortHealth(1), nil)
	if err == nil || !strings.Contains(err.Error(), "inspector") {
		t.Fatalf("expected inspector-required error, got %v", err)
	}
}
