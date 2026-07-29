package process

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Phase 3: the process manager must bind each child's lifetime to the ctx
// passed to Start. When the orchestrator cancels a service's ctx mid-startup,
// every descendant — including grandchildren a shell forked off — must die.
// Before the fix, a stop that arrived between cmd.Start() and process-map
// registration left the child orphaned, which is how dotnet survived a
// dashboard-issued stop during `orbit up`.

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("process-group semantics are unix-specific")
	}
}

// pgidMembers returns the PIDs currently belonging to the process group.
// Uses `pgrep -g` which is available on macOS and linux.
func pgidMembers(pgid int) []string {
	out, _ := exec.Command("pgrep", "-g", strconv.Itoa(pgid)).Output()
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func pgidHasMembers(pgid int) bool { return len(pgidMembers(pgid)) > 0 }

func TestStart_AbortsWhenCtxAlreadyCancelled(t *testing.T) {
	t.Parallel()
	skipOnWindows(t)
	m := NewManager()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Start(ctx, "ghost", ".", "sleep 30", nil, nil, 0)
	if err == nil {
		t.Fatal("Start with pre-cancelled ctx should return an error")
	}
	if m.IsRunning("ghost") {
		t.Error("process should not be registered after cancelled-ctx Start")
	}
}

// writeForkScript drops a shell script that forks a grandchild sleep and
// waits on it. Returns the absolute path. The file is cleaned up via t.
func writeForkScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fork.sh")
	body := "#!/bin/sh\nsleep 30 &\nwait\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func TestStart_KillsProcessTreeOnCtxCancel(t *testing.T) {
	t.Parallel()
	skipOnWindows(t)
	m := NewManager()
	m.CancelGrace = 500 * time.Millisecond // keep test fast

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Launch a shell that forks a grandchild sleep and waits. Killing only
	// the shell would leave the sleep orphaned — this is the zombie shape
	// we are guarding against.
	script := writeForkScript(t)
	if err := m.Start(ctx, "tree", ".", script, nil, nil, 0); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for BOTH the shell and the grandchild sleep to exist in the
	// group. Just checking for any member would pass as soon as the shell
	// itself is placed in the pgid, before the grandchild has forked.
	pgid := -1
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, p, ok := processInfo(m, "tree"); ok && len(pgidMembers(p)) >= 2 {
			pgid = p
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pgid <= 0 {
		t.Fatal("grandchild never joined the process group")
	}

	cancel()

	// Within a short grace window, no process should remain in the pgid.
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !pgidHasMembers(pgid) {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process tree still alive after ctx cancel (pgid=%d)", pgid)
}

func TestExplicitStopAndLifecycleCancelShareOneTermination(t *testing.T) {
	t.Parallel()
	skipOnWindows(t)
	m := NewManager()
	m.CancelGrace = 500 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	if err := m.Start(ctx, "shared-stop", ".", "sleep 30", nil, nil, 0); err != nil {
		t.Fatalf("Start: %v", err)
	}

	cancel()
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			errs <- m.Stop("shared-stop", 500*time.Millisecond)
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Errorf("concurrent stop: %v", err)
		}
	}
	if m.IsAlive("shared-stop") {
		t.Error("process remains alive after converged stop")
	}
}

func TestStopReturnsOnlyAfterReplacementCanBeTracked(t *testing.T) {
	t.Parallel()
	skipOnWindows(t)
	m := NewManager()

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	if err := m.Start(firstCtx, "api", ".", "sleep 30", nil, nil, 1); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	cancelFirst()
	if err := m.Stop("api", 500*time.Millisecond); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	if err := m.Start(secondCtx, "api", ".", "sleep 30", nil, nil, 2); err != nil {
		t.Fatalf("replacement Start: %v", err)
	}
	if !m.IsAlive("api") {
		t.Fatal("replacement is not tracked as alive")
	}
	if err := m.Stop("api", 500*time.Millisecond); err != nil {
		t.Fatalf("replacement Stop: %v", err)
	}
}

func TestStartPublishesOwnershipBeforeReturning(t *testing.T) {
	t.Parallel()
	skipOnWindows(t)
	m := NewManager()
	var callbackPID int
	m.OnStarted = func(name string) {
		pid, _, ok := m.GetProcessInfo(name)
		if ok {
			callbackPID = pid
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx, "api", ".", "sleep 30", nil, nil, 1); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if callbackPID <= 0 {
		t.Fatal("OnStarted ran before process ownership was queryable")
	}
	if err := m.Stop("api", 500*time.Millisecond); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// processInfo is a small accessor wrapper around the manager's internal
// bookkeeping. Manager.GetProcessInfo already exists for the dashboard;
// reuse it here.
func processInfo(m *Manager, name string) (int, int, bool) {
	return m.GetProcessInfo(name)
}
