//go:build !windows

package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/platform"
)

func TestExitReportingDrainsOutputPendingAfterLeaderExit(t *testing.T) {
	manager := NewManager()
	manager.CancelGrace = 3 * time.Second
	ready := make(chan struct{}, 1)
	entered := make(chan struct{}, 1)
	exited := make(chan graceExit, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var mu sync.Mutex
	var lines []string
	manager.OnOutput = func(_ string, line string) {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
		if line == "READY" {
			ready <- struct{}{}
		}
		if line == "HANDLER" {
			entered <- struct{}{}
			<-release
		}
	}
	manager.OnExit = func(_ string, _ int, err error, stderr []string) { exited <- graceExit{err, stderr} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() { unblock(); _ = manager.Stop("drain", 100*time.Millisecond) }()
	gate := filepath.Join(t.TempDir(), "drain-gate")
	env := graceHelperEnvironment("cooperative")
	env["ORBIT_PROCESS_DRAIN_GATE"] = gate
	if err := manager.Start(ctx, "drain", t.TempDir(), graceHelperCommand(), env, nil, 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not become ready")
	}
	pid, _, ok := manager.GetProcessInfo("drain")
	if !ok {
		t.Fatal("process ownership missing")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- manager.Stop("drain", 3*time.Second) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for platform.IsProcessAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatal("child was not reaped while output delivery was paused")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-exited:
		t.Fatal("exit was reported before output delivery completed")
	default:
	}
	unblock()
	var exit graceExit
	select {
	case exit = <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("exit reporting did not finish")
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	output := append([]string(nil), lines...)
	mu.Unlock()
	if exit.err != nil || !containsExactLine(output, "FINAL-OUT") || !containsExactLine(exit.stderr, "FINAL-ERR") {
		t.Fatalf("pending final output lost: error=%v output=%q stderr=%q", exit.err, output, exit.stderr)
	}
}

func TestInheritedOutputDescriptorHasPostExitDrainDeadline(t *testing.T) {
	manager := NewManager()
	manager.CancelGrace = 150 * time.Millisecond
	exited := make(chan graceExit, 1)
	var pgid int
	manager.OnStarted = func(name string) { _, pgid, _ = manager.GetProcessInfo(name) }
	manager.OnExit = func(_ string, _ int, err error, stderr []string) { exited <- graceExit{err, stderr} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		if pgid > 0 && platform.IsGroupAlive(pgid) {
			_ = platform.KillProcessGroup(pgid, syscall.SIGKILL)
		}
	}()
	start := time.Now()
	if err := manager.Start(ctx, "inherited", t.TempDir(), graceHelperCommand(), graceHelperEnvironment("inherited_output"), nil, 1); err != nil {
		t.Fatal(err)
	}
	select {
	case exit := <-exited:
		if !errors.Is(exit.err, ErrOutputDrainTimeout) {
			t.Fatalf("incomplete output not classified: %v", exit.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("inherited output descriptor blocked exit reporting")
	}
	if elapsed := time.Since(start); elapsed < processOutputDrainTimeout {
		t.Fatalf("post-exit drain window not honored: %s", elapsed)
	}
	if manager.IsRunning("inherited") {
		t.Fatal("leader remained tracked after drain deadline")
	}
	assertOwnedProcessGroupGone(t, pgid)
}

func TestStopReturnsIncompleteOutputErrorAfterBoundedDrain(t *testing.T) {
	manager := NewManager()
	ready := make(chan struct{}, 1)
	exited := make(chan graceExit, 1)
	var pid, pgid int
	manager.OnStarted = func(name string) { pid, pgid, _ = manager.GetProcessInfo(name) }
	manager.OnOutput = func(_ string, line string) {
		if line == "READY" {
			ready <- struct{}{}
		}
	}
	manager.OnExit = func(_ string, _ int, err error, stderr []string) { exited <- graceExit{err, stderr} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		if pgid > 0 && platform.IsGroupAlive(pgid) {
			_ = platform.KillProcessGroup(pgid, syscall.SIGKILL)
		}
	}()
	if err := manager.Start(ctx, "drain-stop", t.TempDir(), graceHelperCommand(), graceHelperEnvironment("inherited_output"), nil, 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("owned descriptor holder did not become ready")
	}
	deadline := time.Now().Add(time.Second)
	for platform.IsProcessAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatal("leader did not exit")
		}
		time.Sleep(time.Millisecond)
	}
	if err := manager.Stop("drain-stop", 3*time.Second); !errors.Is(err, ErrOutputDrainTimeout) {
		t.Fatalf("stop hid incomplete output: %v", err)
	}
	select {
	case exit := <-exited:
		if !errors.Is(exit.err, ErrOutputDrainTimeout) {
			t.Fatalf("exit hid incomplete output: %v", exit.err)
		}
	case <-time.After(time.Second):
		t.Fatal("missing exit result")
	}
	assertOwnedProcessGroupGone(t, pgid)
}

func assertOwnedProcessGroupGone(t *testing.T, pgid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for platform.IsGroupAlive(pgid) {
		if time.Now().After(deadline) {
			t.Fatalf("owned process group %d remained alive after exit result", pgid)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDrainTimeoutRetainsOwnershipUntilDescendantGraceFinishes(t *testing.T) {
	manager := NewManager()
	manager.CancelGrace = 3 * time.Second
	exited := make(chan graceExit, 1)
	var pgid int
	manager.OnStarted = func(name string) { _, pgid, _ = manager.GetProcessInfo(name) }
	manager.OnExit = func(_ string, _ int, err error, stderr []string) { exited <- graceExit{err, stderr} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		if pgid > 0 && platform.IsGroupAlive(pgid) {
			_ = platform.KillProcessGroup(pgid, syscall.SIGKILL)
		}
	}()
	gate := filepath.Join(t.TempDir(), "cleanup")
	env := graceHelperEnvironment("inherited_output")
	env["ORBIT_PROCESS_INHERITED_MODE"] = "cleanup_gate"
	env["ORBIT_PROCESS_CLEANUP_GATE"] = gate
	if err := manager.Start(ctx, "cleanup-owner", t.TempDir(), graceHelperCommand(), env, nil, 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(gate + ".ready"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("post-drain cleanup did not give descendant a graceful signal")
		}
		time.Sleep(time.Millisecond)
	}
	if !manager.IsRunning("cleanup-owner") {
		t.Fatal("ownership retired while descendant was still handling termination")
	}
	select {
	case <-exited:
		t.Fatal("exit reported before group cleanup finished")
	default:
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case exit := <-exited:
		if !errors.Is(exit.err, ErrOutputDrainTimeout) {
			t.Fatalf("drain failure lost: %v", exit.err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("graceful group cleanup did not finish")
	}
	assertOwnedProcessGroupGone(t, pgid)
	if manager.IsRunning("cleanup-owner") {
		t.Fatal("ownership not retired after group cleanup")
	}
}
