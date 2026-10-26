package health

import (
	"context"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

func TestStartupScheduleStaysInsideTheIntervalBudget(t *testing.T) {
	c := NewChecker(nil, nil)
	interval, retries := 5*time.Second, 4
	s := c.startupSchedule(&config.HealthCheckConfig{Type: "http"}, interval, retries)

	probes, previous, lastAttempt := 1, s.offset, s.attempt()
	for s.advance() {
		probes++
		if gap := s.offset - previous; gap <= 0 || gap > interval {
			t.Fatalf("gap %v at %v, want within (0, %v]", gap, s.offset, interval)
		}
		if a := s.attempt(); a < lastAttempt || a > retries {
			t.Fatalf("attempt %d after %d, want non-decreasing and at most %d", a, lastAttempt, retries)
		}
		previous, lastAttempt = s.offset, s.attempt()
	}

	if previous != time.Duration(retries-1)*interval || lastAttempt != retries {
		t.Fatalf("last probe at %v attempt %d, want %v attempt %d", previous, lastAttempt, time.Duration(retries-1)*interval, retries)
	}
	if probes <= retries {
		t.Fatalf("probes = %d, want more than retries: the first interval ramps", probes)
	}
}

func TestStartupProbeNoticesAServiceReadyBeforeTheFirstInterval(t *testing.T) {
	c := NewChecker(nil, nil)
	c.startupRampFloor = 20 * time.Millisecond
	start := time.Now()
	probe := func(context.Context) Result {
		return Result{Service: "svc", Healthy: time.Since(start) >= 100*time.Millisecond, Message: "connection refused"}
	}

	hc := &config.HealthCheckConfig{Type: "tcp", Interval: 2 * time.Second, Retries: 3}
	if err := c.pollWithProbe(context.Background(), "svc", hc, nil, probe); err != nil {
		t.Fatal(err)
	}

	if took := time.Since(start); took > time.Second {
		t.Fatalf("ready service detected after %v; the 2s interval should not gate it", took)
	}
	if got := c.Progress("svc").Attempts; got != 1 {
		t.Fatalf("Attempts = %d, want 1: ramp probes stay inside the first interval", got)
	}
}

func TestStartupRampKeepsTheRetryBudget(t *testing.T) {
	c := NewChecker(nil, nil)
	c.startupRampFloor = 5 * time.Millisecond
	calls := 0
	probe := func(context.Context) Result {
		calls++
		return Result{Service: "svc", Message: "connection refused"}
	}

	hc := &config.HealthCheckConfig{Type: "http", Interval: 60 * time.Millisecond, Retries: 3}
	start := time.Now()
	if err := c.pollWithProbe(context.Background(), "svc", hc, nil, probe); err == nil {
		t.Fatal("a never-ready service must fail once the budget is spent")
	}

	if took := time.Since(start); took < 120*time.Millisecond {
		t.Fatalf("gave up after %v, before the retries×interval budget", took)
	}
	if calls <= 3 {
		t.Fatalf("probes = %d, want more than retries: the ramp probes between ticks", calls)
	}
	if got := c.Progress("svc"); got.Attempts != 3 || got.MaxRetries != 3 {
		t.Fatalf("progress = %+v, want attempts 3/3", got)
	}
}

func TestExecStartupProbesKeepThePlainInterval(t *testing.T) {
	c := NewChecker(nil, nil)
	c.startupRampFloor = time.Millisecond
	calls := 0
	probe := func(context.Context) Result {
		calls++
		return Result{Service: "db", Message: "not ready"}
	}

	hc := &config.HealthCheckConfig{Type: "exec", Interval: 30 * time.Millisecond, Retries: 3}
	_ = c.pollWithProbe(context.Background(), "db", hc, nil, probe)

	if calls != 3 {
		t.Fatalf("exec probes = %d, want 3: each one is a docker exec, so no ramp", calls)
	}
}

func TestSlowStartupProbesSpendTheRetryBudgetNotTheRamp(t *testing.T) {
	c := NewChecker(nil, nil)
	c.startupRampFloor = 5 * time.Millisecond
	calls := 0
	probe := func(context.Context) Result {
		calls++
		time.Sleep(50 * time.Millisecond)
		return Result{Service: "svc", Message: "timeout"}
	}

	hc := &config.HealthCheckConfig{Type: "http", Interval: 40 * time.Millisecond, Retries: 3}
	_ = c.pollWithProbe(context.Background(), "svc", hc, nil, probe)

	if calls != 3 {
		t.Fatalf("probes = %d, want 3: a probe slower than the ramp must not queue ramp probes behind it", calls)
	}
}
