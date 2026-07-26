package health

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/logging"
)

func newLogChecker() (*Checker, *logging.Multiplexer) {
	mux := logging.NewMultiplexer()
	return NewChecker(mux, nil), mux
}

func TestWaitForLog_Matches(t *testing.T) {
	checker, mux := newLogChecker()
	hc := &config.HealthCheckConfig{Type: "log", Pattern: "Recovery is complete", Timeout: 2 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- checker.WaitForHealthy(ctx, "sql-server", hc, nil) }()

	// Give the goroutine time to subscribe.
	time.Sleep(10 * time.Millisecond)

	mux.Write("sql-server", "Starting up database 'master'")
	mux.Write("sql-server", "Recovery is complete. This is an informational message only")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForHealthy did not return after matching line")
	}
}

func TestWaitForLog_IgnoresOtherServices(t *testing.T) {
	checker, mux := newLogChecker()
	hc := &config.HealthCheckConfig{Type: "log", Pattern: "ready", Timeout: 200 * time.Millisecond}

	done := make(chan error, 1)
	go func() { done <- checker.WaitForHealthy(context.Background(), "target", hc, nil) }()

	time.Sleep(10 * time.Millisecond)
	mux.Write("other-service", "ready") // wrong service — must not unblock

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected timeout, got nil (wrong-service line leaked through)")
		}
		if !strings.Contains(err.Error(), "not seen") {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout should have fired by now")
	}
}

func TestWaitForLog_Timeout(t *testing.T) {
	checker, _ := newLogChecker()
	hc := &config.HealthCheckConfig{Type: "log", Pattern: "never-emitted", Timeout: 100 * time.Millisecond}

	err := checker.WaitForHealthy(context.Background(), "svc", hc, nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "not seen") {
		t.Errorf("expected 'not seen' in error, got %v", err)
	}
}

func TestWaitForLog_ContextCancel(t *testing.T) {
	checker, _ := newLogChecker()
	hc := &config.HealthCheckConfig{Type: "log", Pattern: "x", Timeout: 10 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- checker.WaitForHealthy(ctx, "svc", hc, nil) }()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancel did not propagate")
	}
}

func TestWaitForLog_UnsubscribesAfterMatch(t *testing.T) {
	checker, mux := newLogChecker()
	hc := &config.HealthCheckConfig{Type: "log", Pattern: "ready", Timeout: 2 * time.Second}

	done := make(chan error, 1)
	go func() { done <- checker.WaitForHealthy(context.Background(), "svc", hc, nil) }()

	time.Sleep(10 * time.Millisecond)
	mux.Write("svc", "ready")
	<-done

	// Multiplexer should have no subscribers left; a later Write must not deadlock
	// or feed back into the (already-returned) wait.
	if n := mux.SubscriberCount(); n != 0 {
		t.Errorf("expected 0 subscribers after match, got %d", n)
	}
}

func TestWaitForLog_InvalidPattern(t *testing.T) {
	checker, _ := newLogChecker()
	hc := &config.HealthCheckConfig{Type: "log", Pattern: "(unclosed", Timeout: time.Second}

	err := checker.WaitForHealthy(context.Background(), "svc", hc, nil)
	if err == nil {
		t.Fatal("expected regex compile error, got nil")
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Errorf("error should mention pattern, got %v", err)
	}
}
