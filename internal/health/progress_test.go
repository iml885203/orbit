package health

import (
	"context"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
)

func TestProgress_WaitForHealthyRecordsAttemptsAndClearsOnSuccess(t *testing.T) {
	c := NewChecker(nil, nil)

	// Force probe to fail twice then succeed, so we observe attempt
	// counter incrementing and LastErr clearing on the final attempt. The
	// 10ms interval sits below the startup ramp floor, so every probe is its
	// own interval tick.
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

func TestProgress_ShowsProbeInFlightUntilItReturns(t *testing.T) {
	c := NewChecker(nil, nil)
	hc := &config.HealthCheckConfig{Type: "http", Interval: time.Second, Retries: 1}
	entered := make(chan struct{})
	release := make(chan struct{})
	probe := func(ctx context.Context) Result {
		close(entered)
		<-release
		return Result{Service: "svc", Healthy: true}
	}
	done := make(chan error, 1)
	go func() { done <- c.pollWithProbe(context.Background(), "svc", hc, nil, probe) }()

	await(t, entered, "probe to start")
	inFlight := c.Progress("svc")
	if !inFlight.Configured || inFlight.ProbeStartedAt.IsZero() {
		t.Fatalf("hung probe must be visible as in flight, got %+v", inFlight)
	}
	if !inFlight.LastProbeAt.IsZero() || inFlight.Attempts != 0 {
		t.Fatalf("no probe has returned yet, got %+v", inFlight)
	}

	close(release)
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pollWithProbe never returned")
	}
	if err != nil {
		t.Fatalf("pollWithProbe: %v", err)
	}
	finished := c.Progress("svc")
	if !finished.ProbeStartedAt.IsZero() {
		t.Fatalf("returned probe still marked in flight: %+v", finished)
	}
	if finished.LastProbeAt.Before(inFlight.ProbeStartedAt) {
		t.Fatalf("LastProbeAt %v precedes probe start %v", finished.LastProbeAt, inFlight.ProbeStartedAt)
	}
}

// A result callback that never returns (the lifecycle event loop stalled)
// must not hide the probe's verdict from status.
func TestProgress_RecordsVerdictBeforeDeliveringResult(t *testing.T) {
	c := NewChecker(nil, nil)
	hc := &config.HealthCheckConfig{Type: "http", Interval: time.Second, Retries: 3}
	probe := func(ctx context.Context) Result {
		return Result{Service: "svc", Healthy: true}
	}
	var seen Progress
	onResult := func(Result) { seen = c.Progress("svc") }
	if err := c.pollWithProbe(context.Background(), "svc", hc, onResult, probe); err != nil {
		t.Fatalf("pollWithProbe: %v", err)
	}
	if seen.Attempts != 1 || seen.LastErr != "" || seen.LastProbeAt.IsZero() || !seen.ProbeStartedAt.IsZero() {
		t.Fatalf("callback saw %+v, want the successful verdict already recorded", seen)
	}
}

func TestProgress_ProbeFromAnEarlierStartCannotClearItsSuccessor(t *testing.T) {
	c := NewChecker(nil, nil)
	staleEntered := make(chan struct{})
	staleRelease := make(chan struct{})
	staleDone := make(chan struct{})
	go func() {
		defer close(staleDone)
		c.trackProbe("svc", func() Result {
			close(staleEntered)
			<-staleRelease
			return Result{Service: "svc", Healthy: true, Protocol: "HTTP/1.1"}
		})
	}()
	await(t, staleEntered, "stale probe to start")

	// The service restarts: its new startup poll resets progress and starts
	// a probe of its own that is still running when the stale one returns.
	c.resetProgress("svc")
	currentEntered := make(chan struct{})
	currentRelease := make(chan struct{})
	currentDone := make(chan struct{})
	go func() {
		defer close(currentDone)
		c.trackProbe("svc", func() Result {
			close(currentEntered)
			<-currentRelease
			return Result{Service: "svc", Healthy: true}
		})
	}()
	await(t, currentEntered, "current probe to start")
	close(staleRelease)
	await(t, staleDone, "stale probe to finish")

	got := c.Progress("svc")
	if got.ProbeStartedAt.IsZero() || !got.LastProbeAt.IsZero() || got.Protocol != "" {
		t.Fatalf("stale probe overwrote its successor's progress: %+v", got)
	}
	close(currentRelease)
	await(t, currentDone, "current probe to finish")
}

func await(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}
