package health

import (
	"context"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

func TestProgress_WaitForHealthyRecordsAttemptsAndClearsOnSuccess(t *testing.T) {
	c := NewChecker(nil, nil)

	// Force probe to fail twice then succeed, so we observe attempt
	// counter incrementing and LastErr clearing on the final attempt.
	calls := 0
	probe := func(ctx context.Context) Result {
		calls++
		if calls < 3 {
			return Result{Service: "svc", Healthy: false, Message: "connection refused"}
		}
		return Result{Service: "svc", Healthy: true}
	}

	hc := &config.HealthCheckConfig{Type: "http", Interval: 10 * time.Millisecond, Retries: 5}
	if err := c.pollWithProbe(context.Background(), "svc", hc, nil, probe); err != nil {
		t.Fatalf("pollWithProbe: %v", err)
	}

	got := c.Progress("svc")
	if !got.Configured {
		t.Errorf("Configured = false, want true")
	}
	if got.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", got.Attempts)
	}
	if got.LastErr != "" {
		t.Errorf("LastErr should clear on final success, got %q", got.LastErr)
	}
}

func TestProgress_WaitForHealthyResetsBetweenCalls(t *testing.T) {
	c := NewChecker(nil, nil)
	hc := &config.HealthCheckConfig{Type: "http", Interval: 10 * time.Millisecond, Retries: 2}

	// First run: exhausts all retries with a distinctive error so we
	// know Attempts==2 and LastErr=="boom" are left in the tracker.
	probe := func(ctx context.Context) Result {
		return Result{Service: "svc", Healthy: false, Message: "boom"}
	}
	if err := c.pollWithProbe(context.Background(), "svc", hc, nil, probe); err == nil {
		t.Fatalf("first run should fail (retries exhausted)")
	}
	if got := c.Progress("svc"); got.Attempts != 2 || got.LastErr != "boom" {
		t.Fatalf("after first run, want Attempts=2 LastErr=%q, got %+v", "boom", got)
	}

	// Second run: a probe that, on its first call, checks the tracker
	// has been reset. Without resetProgress in pollWithProbe, the probe
	// would still observe the stale Attempts=2 / LastErr="boom" because
	// recordProgress runs *after* the probe.
	var insideProgress Progress
	probe = func(ctx context.Context) Result {
		insideProgress = c.Progress("svc")
		return Result{Service: "svc", Healthy: true}
	}
	if err := c.pollWithProbe(context.Background(), "svc", hc, nil, probe); err != nil {
		t.Fatalf("second pollWithProbe: %v", err)
	}
	if insideProgress.Attempts != 0 || insideProgress.LastErr != "" {
		t.Errorf("resetProgress should clear state before first probe, got %+v inside probe", insideProgress)
	}
}
