package process

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFailedRetirementKeepsIdentityAndExplicitStopCanRetry(t *testing.T) {
	manager := NewManager()
	failure := errors.New("controlled process-tree signal failure")
	var recoverable atomic.Bool
	manager.terminateGroup = func(pgid int, grace time.Duration) error {
		if !recoverable.Load() {
			return failure
		}
		return KillGroup(pgid, grace)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	// The short helper returns naturally so the controlled failure is at the
	// cleanup boundary; it does not require a leaked real descendant.
	command := executionCommand("quick")
	gate := dir + "/ready"
	env := map[string]string{"ORBIT_EXECUTION_HELPER": "1", "ORBIT_EXECUTION_GATE": gate}
	manager.OnOwnershipChanged = func(name string) {
		if _, ok := manager.Ownership(name); ok {
			_ = os.WriteFile(gate, nil, 0600)
		}
	}
	err := manager.Start(ctx, "api", dir, executionCommand("main"), env, []string{command}, 1)
	if !errors.Is(err, failure) {
		t.Fatalf("preparation failure=%v", err)
	}
	retained, ok := manager.Ownership("api")
	if !ok || retained.PID <= 0 || retained.Command != command || retained.Stage != "pre_start" {
		t.Fatalf("retirement identity lost: %+v %v", retained, ok)
	}
	if !manager.IsRunning("api") {
		t.Fatal("failed retirement reservation lost")
	}
	if err := manager.Start(ctx, "api", dir, executionCommand("main"), env, nil, 2); err == nil {
		t.Fatal("replacement admitted while cleanup unproven")
	}
	if err := manager.Stop("api", 0); !errors.Is(err, failure) {
		t.Fatalf("retry failure=%v", err)
	}
	if current, ok := manager.Ownership("api"); !ok || current != retained {
		t.Fatal("failed retry erased identity")
	}
	recoverable.Store(true)
	retries := make(chan error, 8)
	for range 8 {
		go func() { retries <- manager.Stop("api", 0) }()
	}
	for range 8 {
		if err := <-retries; err != nil {
			t.Fatal(err)
		}
	}

	if manager.IsRunning("api") {
		t.Fatal("successful retry did not remove reservation")
	}
	if _, ok := manager.Ownership("api"); ok {
		t.Fatal("successful retry retained old child")
	}
	if err := manager.Start(ctx, "api", dir, executionCommand("main"), env, nil, 2); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop("api", 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestLivePreparationTerminationFailureCanBeRetried(t *testing.T) {
	manager := NewManager()
	manager.CancelGrace = 100 * time.Millisecond
	failure := errors.New("controlled live-group signal failure")
	var recovered atomic.Bool
	manager.terminateGroup = func(pgid int, grace time.Duration) error {
		if !recovered.Load() {
			return failure
		}
		return KillGroup(pgid, grace)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	gate := dir + "/spawn"
	ready := make(chan struct{})
	var once sync.Once
	manager.OnOutput = func(_ string, line string) {
		if line == "PREPARED" {
			once.Do(func() { close(ready) })
		}
	}
	env := map[string]string{"ORBIT_EXECUTION_HELPER": "1", "ORBIT_EXECUTION_GATE": gate}
	done := make(chan error, 1)
	go func() {
		done <- manager.Start(ctx, "api", dir, executionCommand("main"), env, []string{executionCommand("tree")}, 1)
	}()
	t.Cleanup(func() { recovered.Store(true); cancel(); _ = manager.Stop("api", 100*time.Millisecond) })
	awaitExecution(t, ready)
	original, ok := manager.Ownership("api")
	if !ok {
		t.Fatal("missing preparation")
	}
	if err := manager.Stop("api", 100*time.Millisecond); !errors.Is(err, failure) {
		t.Fatalf("first Stop=%v", err)
	}
	if !manager.IsAlive("api") {
		t.Fatal("fault did not retain live leader")
	}
	if observed, ok := manager.Ownership("api"); !ok || observed != original {
		t.Fatal("failed live stop lost identity")
	}
	recovered.Store(true)
	if err := manager.Stop("api", 100*time.Millisecond); err != nil {
		t.Fatal("retry:", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("live preparation did not retire on retry")
	}
	if manager.IsRunning("api") {
		t.Fatal("retired preparation still blocks replacement")
	}
	if err := manager.Start(context.Background(), "api", dir, executionCommand("main"), env, nil, 2); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop("api", 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestDelayedOldStopCannotTerminateReplacement(t *testing.T) {
	manager := NewManager()
	failure := errors.New("controlled cleanup failure")
	var recovered atomic.Bool
	manager.terminateGroup = func(pgid int, grace time.Duration) error {
		if !recovered.Load() {
			return failure
		}
		return KillGroup(pgid, grace)
	}
	dir := t.TempDir()
	gate := dir + "/ready"
	_ = os.WriteFile(gate, nil, 0600)
	env := map[string]string{"ORBIT_EXECUTION_HELPER": "1", "ORBIT_EXECUTION_GATE": gate}
	if err := manager.Start(context.Background(), "api", dir, executionCommand("main"), env, []string{executionCommand("instant")}, 1); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	// Pin the authority captured by a delayed public Stop. The concurrency
	// invariant is that a same-name lookup cannot replace this exact token.
	manager.mu.RLock()
	oldOwner := manager.executions["api"]
	manager.mu.RUnlock()
	captured := make(chan struct{})
	release := make(chan struct{})
	late := make(chan error, 1)
	go func() { close(captured); <-release; late <- manager.stopExecution("api", oldOwner, 0) }()
	<-captured
	recovered.Store(true)
	if err := manager.Stop("api", 0); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background(), "api", dir, executionCommand("main"), env, nil, 2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop("api", 100*time.Millisecond) })
	replacement, ok := manager.Ownership("api")
	if !ok {
		t.Fatal("replacement not registered")
	}
	close(release)
	if err := <-late; err != nil {
		t.Fatal(err)
	}
	if now, ok := manager.Ownership("api"); !ok || now != replacement || !manager.IsAlive("api") {
		t.Fatal("delayed old Stop touched replacement")
	}
}
