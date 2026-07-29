package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

// A service whose first requests fail must flip healthy once it recovers —
// the whole point of recovery probing is that a spent startup budget is not
// a terminal verdict.
func TestRecoverHealthy_FlipsHealthyAfterWarmup(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker := NewChecker(nil, nil)
	checker.recoveryInterval = 10 * time.Millisecond
	hc := &config.HealthCheckConfig{Type: "http", Port: portFromURL(t, srv.URL), Retries: 5}

	var recovered atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := checker.RecoverHealthy(ctx, "warmup-svc", 1, hc, func(r Result) {
		if r.Healthy {
			recovered.Store(true)
		}
	}); err != nil {
		t.Fatalf("expected recovery, got %v", err)
	}
	if !recovered.Load() {
		t.Fatal("onResult was not called with a healthy result")
	}

	prog := checker.Progress("warmup-svc")
	if prog.LastErr != "" {
		t.Fatalf("expected LastErr cleared on recovery, got %q", prog.LastErr)
	}
	if prog.Recovering {
		t.Fatal("expected Recovering cleared once the probe succeeds")
	}
}

// While the loop runs, progress must advertise that recovery probing is
// active (CLI waits key off this to treat degraded as non-terminal) — and
// the flag must be cleared once the loop exits without success, so a ghost
// Recovering can't make a truly terminal degraded look like still-trying.
func TestRecoverHealthy_FlagsWhileRunning_ClearsOnExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	checker := NewChecker(nil, nil)
	checker.recoveryInterval = 10 * time.Millisecond
	hc := &config.HealthCheckConfig{Type: "http", Port: portFromURL(t, srv.URL), Retries: 5}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- checker.RecoverHealthy(ctx, "flagged-svc", 1, hc, nil) }()

	deadline := time.Now().Add(2 * time.Second)
	for !checker.Progress("flagged-svc").Recovering {
		if time.Now().After(deadline) {
			t.Fatal("Recovering never flagged while probes keep failing")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if checker.Progress("flagged-svc").Recovering {
		t.Fatal("expected Recovering cleared after the loop exits without success")
	}
}

func TestRecoverHealthy_CancelledContextStopsProbing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	checker := NewChecker(nil, nil)
	checker.recoveryInterval = 10 * time.Millisecond
	hc := &config.HealthCheckConfig{Type: "http", Port: portFromURL(t, srv.URL), Retries: 5}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if err := checker.RecoverHealthy(ctx, "never-svc", 1, hc, nil); err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// A cancelled loop from a previous start must not drop the flag its
// successor generation just raised — clears are owner-scoped.
func TestClearRecovering_OldOwnerCannotClearNewFlag(t *testing.T) {
	checker := NewChecker(nil, nil)

	checker.MarkRecovering("svc", 2)
	checker.clearRecovering("svc", 1)
	if !checker.Progress("svc").Recovering {
		t.Fatal("old-generation clear dropped the new generation's Recovering flag")
	}

	checker.clearRecovering("svc", 2)
	if checker.Progress("svc").Recovering {
		t.Fatal("same-generation clear should drop the flag")
	}
}

// Strategies without a meaningful repeatable probe must be rejected with the
// sentinel — a nil here would read as "recovered" at the call site.
func TestRecoverHealthy_UnsupportedStrategiesReturnSentinel(t *testing.T) {
	checker := NewChecker(nil, nil)
	checker.recoveryInterval = time.Millisecond

	for _, hc := range []*config.HealthCheckConfig{
		nil,
		{Type: "log", Pattern: "ready"},
		{Type: "unknown"},
	} {
		if SupportsRecovery(hc) {
			t.Fatalf("expected SupportsRecovery=false for %+v", hc)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		if err := checker.RecoverHealthy(ctx, "noop-svc", 1, hc, nil); !errors.Is(err, ErrRecoveryUnsupported) {
			t.Fatalf("expected ErrRecoveryUnsupported for %+v, got %v", hc, err)
		}
		cancel()
	}
}

func TestMonitorHealthy_DebouncesFailuresAndRecovers(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	checker := NewChecker(nil, nil)
	hc := &config.HealthCheckConfig{
		Type:             "http",
		Port:             portFromURL(t, srv.URL),
		Interval:         10 * time.Millisecond,
		FailureThreshold: 3,
	}
	results := make(chan Result, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- checker.MonitorHealthy(ctx, "api", hc, func(result Result) {
			results <- result
		})
	}()

	select {
	case result := <-results:
		if result.Healthy {
			t.Fatalf("first verdict = healthy, want degraded")
		}
	case <-time.After(time.Second):
		t.Fatal("monitor did not report the third consecutive failure")
	}
	healthy.Store(true)
	select {
	case result := <-results:
		if !result.Healthy {
			t.Fatalf("recovery verdict = unhealthy: %s", result.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("monitor did not report recovery")
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("MonitorHealthy() = %v, want context.Canceled", err)
	}
}

func TestMonitorHealthy_OneTransientFailureDoesNotFlap(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker := NewChecker(nil, nil)
	hc := &config.HealthCheckConfig{
		Type:             "http",
		Port:             portFromURL(t, srv.URL),
		Interval:         10 * time.Millisecond,
		FailureThreshold: 3,
	}
	results := make(chan Result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	err := checker.MonitorHealthy(ctx, "api", hc, func(result Result) {
		results <- result
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("MonitorHealthy() = %v, want deadline exceeded", err)
	}
	select {
	case result := <-results:
		t.Fatalf("transient failure emitted verdict: %+v", result)
	default:
	}
}
