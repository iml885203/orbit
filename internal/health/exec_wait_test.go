package health

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

// fakeInspector is a hand-rolled ContainerInspector double for unit tests.
// Exec and Health are separate queues so tests can script per-call returns.
type fakeInspector struct {
	mu           sync.Mutex
	execReturn   []execResult
	execCalls    int32
	execCalled   chan struct{}
	healthQ      []healthResult
	healthErr    error
	healthCalled chan struct{}
}

type execResult struct {
	code int
	err  error
}

type healthResult struct {
	status string
	err    error
}

func (f *fakeInspector) ExecInContainer(_ context.Context, _ string, _ []string) (int, error) {
	atomic.AddInt32(&f.execCalls, 1)
	if f.execCalled != nil {
		select {
		case f.execCalled <- struct{}{}:
		default:
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.execReturn) == 0 {
		return 0, errors.New("fake: exec queue empty")
	}
	r := f.execReturn[0]
	if len(f.execReturn) > 1 {
		f.execReturn = f.execReturn[1:]
	}
	return r.code, r.err
}

func (f *fakeInspector) HealthStatus(_ context.Context, _ string) (string, error) {
	if f.healthCalled != nil {
		select {
		case f.healthCalled <- struct{}{}:
		default:
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.healthErr != nil {
		return "", f.healthErr
	}
	if len(f.healthQ) == 0 {
		return "healthy", nil
	}
	r := f.healthQ[0]
	if len(f.healthQ) > 1 {
		f.healthQ = f.healthQ[1:]
	}
	return r.status, r.err
}

func shortExec(cmd []string, retries int) *config.HealthCheckConfig {
	return &config.HealthCheckConfig{
		Type:     "exec",
		Command:  cmd,
		Interval: time.Millisecond,
		Retries:  retries,
	}
}

func TestWaitForExec_SucceedsOnFirstTry(t *testing.T) {
	insp := &fakeInspector{execReturn: []execResult{{code: 0}}}
	c := NewChecker(nil, insp)
	hc := shortExec([]string{"true"}, 5)

	if err := c.WaitForHealthy(context.Background(), "svc", hc, nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if atomic.LoadInt32(&insp.execCalls) != 1 {
		t.Errorf("expected 1 exec call, got %d", insp.execCalls)
	}
}

func TestWaitForExec_RetriesUntilZero(t *testing.T) {
	insp := &fakeInspector{execReturn: []execResult{{code: 1}, {code: 1}, {code: 0}}}
	c := NewChecker(nil, insp)
	hc := shortExec([]string{"sqlcmd"}, 10)

	if err := c.WaitForHealthy(context.Background(), "svc", hc, nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := atomic.LoadInt32(&insp.execCalls); got != 3 {
		t.Errorf("expected 3 exec calls, got %d", got)
	}
}

func TestWaitForExec_RetriesExhausted(t *testing.T) {
	insp := &fakeInspector{execReturn: []execResult{{code: 1}}} // always fails
	c := NewChecker(nil, insp)
	hc := shortExec([]string{"false"}, 3)

	err := c.WaitForHealthy(context.Background(), "svc", hc, nil)
	if err == nil {
		t.Fatal("expected exhaustion error, got nil")
	}
	if !strings.Contains(err.Error(), "failed after") {
		t.Errorf("unexpected err: %v", err)
	}
}

func TestWaitForExec_ContextCancel(t *testing.T) {
	insp := &fakeInspector{
		execReturn: []execResult{{code: 1}},
		execCalled: make(chan struct{}, 1),
	}
	c := NewChecker(nil, insp)
	hc := &config.HealthCheckConfig{Type: "exec", Command: []string{"x"}, Interval: 10 * time.Millisecond, Retries: 1000}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.WaitForHealthy(ctx, "svc", hc, nil) }()

	select {
	case <-insp.execCalled:
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

func TestWaitForExec_MissingInspector(t *testing.T) {
	c := NewChecker(nil, nil) // no inspector
	hc := shortExec([]string{"x"}, 1)
	err := c.WaitForHealthy(context.Background(), "svc", hc, nil)
	if err == nil || !strings.Contains(err.Error(), "inspector") {
		t.Fatalf("expected inspector-required error, got %v", err)
	}
}

func TestWaitForExec_MissingCommand(t *testing.T) {
	c := NewChecker(nil, &fakeInspector{})
	hc := &config.HealthCheckConfig{Type: "exec", Interval: 10 * time.Millisecond, Retries: 1}
	err := c.WaitForHealthy(context.Background(), "svc", hc, nil)
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Fatalf("expected command-required error, got %v", err)
	}
}
