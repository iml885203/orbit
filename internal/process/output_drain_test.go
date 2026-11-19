//go:build !windows

package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestInheritedOutputDescriptorClosesAfterGroupCleanup(t *testing.T) {
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
	if err := manager.Start(ctx, "inherited", t.TempDir(), graceHelperCommand(), graceHelperEnvironment("inherited_output"), nil, 1); err != nil {
		t.Fatal(err)
	}
	select {
	case exit := <-exited:
		if exit.err != nil {
			t.Fatalf("group cleanup failed: %v", exit.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("inherited output descriptor blocked exit reporting")
	}
	if manager.IsRunning("inherited") {
		t.Fatal("leader remained tracked after drain deadline")
	}
	assertOwnedProcessGroupGone(t, pgid)
}

func TestStopJoinsInheritedDescriptorGroupCleanup(t *testing.T) {
	manager := NewManager()
	manager.CancelGrace = 150 * time.Millisecond
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
	if err := manager.Stop("drain-stop", 3*time.Second); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	select {
	case exit := <-exited:
		if exit.err != nil {
			t.Fatalf("exit failed: %v", exit.err)
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

func TestDescendantGracePrecedesOutputDrainDeadline(t *testing.T) {
	manager := NewManager()
	finalOutput := make(chan struct{}, 1)
	manager.OnOutput = func(_ string, line string) {
		if line == "FINAL-OUT" {
			finalOutput <- struct{}{}
		}
	}
	manager.CancelGrace = 6 * time.Second
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
	time.Sleep(3 * time.Second)
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case exit := <-exited:
		if exit.err != nil || !containsExactLine(exit.stderr, "FINAL-ERR") {
			t.Fatalf("graceful descendant output lost: error=%v stderr=%q", exit.err, exit.stderr)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("graceful group cleanup did not finish")
	}
	select {
	case <-finalOutput:
	default:
		t.Fatal("final descendant stdout lost")
	}
	assertOwnedProcessGroupGone(t, pgid)
	if manager.IsRunning("cleanup-owner") {
		t.Fatal("ownership not retired after group cleanup")
	}
}

func TestOutputDrainDeadlineClosesOwnedReaders(t *testing.T) {
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdoutWriter.Close()
	stderr, stderrWriter, err := os.Pipe()
	if err != nil {
		stdout.Close()
		t.Fatal(err)
	}
	defer stderrWriter.Close()
	completed := make(chan struct{}, 2)
	for _, reader := range []*os.File{stdout, stderr} {
		go func() { var b [1]byte; _, _ = reader.Read(b[:]); completed <- struct{}{} }()
	}
	start := time.Now()
	if err := waitForProcessOutput(completed, stdout, stderr); !errors.Is(err, ErrOutputDrainTimeout) {
		t.Fatalf("missing incomplete output classification: %v", err)
	}
	if elapsed := time.Since(start); elapsed < processOutputDrainTimeout || elapsed > processOutputDrainTimeout+time.Second {
		t.Fatalf("unexpected EOF budget: %s", elapsed)
	}
}

func TestStopSurfacesEscapedDescriptorDrainTimeout(t *testing.T) {
	manager := NewManager()
	manager.CancelGrace = 100 * time.Millisecond
	descendant := make(chan int, 1)
	exited := make(chan error, 1)
	manager.OnOutput = func(_ string, line string) {
		if value, ok := strings.CutPrefix(line, "DESCENDANT="); ok {
			pid, err := strconv.Atoi(value)
			if err == nil {
				descendant <- pid
			}
		}
	}
	manager.OnExit = func(_ string, _ int, err error, _ []string) { exited <- err }
	env := graceHelperEnvironment("inherited_output")
	env["ORBIT_PROCESS_ESCAPE_GROUP"] = "1"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.Start(ctx, "escaped-output", t.TempDir(), graceHelperCommand(), env, nil, 1); err != nil {
		t.Fatal(err)
	}
	var pid int
	select {
	case pid = <-descendant:
	case <-time.After(5 * time.Second):
		t.Fatal("missing owned test descendant")
	}
	defer func() { _ = platform.KillProcessGroup(pid, syscall.SIGKILL) }()
	if err := manager.Stop("escaped-output", time.Second); !errors.Is(err, ErrOutputDrainTimeout) {
		t.Fatalf("stop lost output failure: %v", err)
	}
	select {
	case err := <-exited:
		if !errors.Is(err, ErrOutputDrainTimeout) {
			t.Fatalf("exit lost output failure: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("missing exit")
	}
	if !platform.IsProcessAlive(pid) {
		t.Fatal("manager terminated a process outside its owned group")
	}
}
