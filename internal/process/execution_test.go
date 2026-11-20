package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/platform"
)

func TestExecutionProcessHelper(t *testing.T) {
	if os.Getenv("ORBIT_EXECUTION_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "resume_failure" || mode == "cancel_before_resume" {
		manager := NewManager()
		failure := fmt.Errorf("controlled resume failure")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if mode == "resume_failure" {
			manager.resumeProcess = func(int) error { return failure }
		} else {
			failure = context.Canceled
			manager.OnOwnershipChanged = func(name string) {
				if _, ok := manager.Ownership(name); ok {
					cancel()
				}
			}
		}
		var callbacks atomic.Int32
		manager.OnStarted = func(string) { callbacks.Add(1) }
		manager.OnExit = func(string, int, error, []string) { callbacks.Add(1) }
		err := manager.Start(ctx, "resume", os.Getenv("ORBIT_EXECUTION_DIR"), executionCommand("main"), map[string]string{"ORBIT_EXECUTION_HELPER": "1"}, nil, 1)
		if !errors.Is(err, failure) || manager.IsRunning("resume") || callbacks.Load() != 0 {
			t.Fatalf("resume failure: err=%v running=%v callbacks=%d", err, manager.IsRunning("resume"), callbacks.Load())
		}
		return
	}
	if mode == "instant" {
		return
	}

	if mode == "owner_crash" {
		dir := os.Getenv("ORBIT_EXECUTION_DIR")
		manager := NewManager()
		manager.OnOwnershipChanged = func(name string) {
			if owned, ok := manager.Ownership(name); ok && owned.Stage == "pre_start" {
				_ = os.WriteFile(os.Getenv("ORBIT_EXECUTION_GATE"), nil, 0600)
			}
		}
		manager.OnOutput = func(_ string, line string) {
			fmt.Println(line)
			if line == "PREPARED" {
				os.Exit(0)
			}
		}
		if err := manager.Start(context.Background(), "crash", dir, executionCommand("main"), map[string]string{"ORBIT_EXECUTION_HELPER": "1", "ORBIT_EXECUTION_GATE": os.Getenv("ORBIT_EXECUTION_GATE")}, []string{executionCommand("tree")}, 1); err != nil {
			t.Fatal(err)
		}
		t.Fatal("owner should exit during preparation")
	}

	if mode == "main" {
		fmt.Println("MAIN")
		time.Sleep(10 * time.Second)
		return
	}
	if mode == "child" {
		fmt.Println("CHILD")
		time.Sleep(10 * time.Second)
		return
	}
	deadline := time.Now().Add(5 * time.Second)
	for mode != "tree" {
		if _, err := os.Stat(os.Getenv("ORBIT_EXECUTION_GATE")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("spawn gate closed")
		}
		time.Sleep(time.Millisecond)
	}
	if mode == "tree" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestExecutionProcessHelper$", "-test.timeout=15s", "--", "child")
		cmd.Stdout, cmd.Stderr, cmd.Env = os.Stdout, os.Stderr, os.Environ()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("CHILD-PID=%d\n", cmd.Process.Pid)
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
		fmt.Println("PREPARED")
		time.Sleep(10 * time.Second)
	}
}

func executionCommand(stage string) string {
	return strconv.Quote(os.Args[0]) + " -test.run=^TestExecutionProcessHelper$ -test.timeout=15s -- " + stage
}

func awaitExecution(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("execution gate timed out")
	}
}

func TestPreparingExecutionOwnsQueriesTreeAndReplacement(t *testing.T) {
	for _, action := range []string{"stop", "cancel", "stop_all"} {
		t.Run(action, func(t *testing.T) {
			manager := NewManager()
			manager.CancelGrace = 100 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			dir := t.TempDir()
			gate := filepath.Join(dir, "spawn")
			env := map[string]string{"ORBIT_EXECUTION_HELPER": "1", "ORBIT_EXECUTION_GATE": gate, "ORBIT_STAGE": "tree"}
			pre := executionCommand("${ORBIT_STAGE}")
			ready := make(chan struct{})
			var readyOnce sync.Once
			childPID := make(chan int, 1)
			var mainCalls, exitCalls atomic.Int32
			manager.OnStarted = func(string) { mainCalls.Add(1) }
			manager.OnExit = func(string, int, error, []string) { exitCalls.Add(1) }
			manager.OnOutput = func(_ string, line string) {
				if line == "PREPARED" {
					readyOnce.Do(func() { close(ready) })
				}
				if strings.HasPrefix(line, "CHILD-PID=") {
					pid, _ := strconv.Atoi(strings.TrimPrefix(line, "CHILD-PID="))
					childPID <- pid
				}
			}
			manager.OnOwnershipChanged = func(name string) {
				snap, ok := manager.Ownership(name)
				if ok && snap.Stage == "pre_start" {
					_ = os.WriteFile(gate, nil, 0600)
				}
			}
			started := make(chan error, 1)
			go func() { started <- manager.Start(ctx, "api", dir, executionCommand("main"), env, []string{pre}, 7) }()
			pid := 0
			t.Cleanup(func() {
				cancel()
				if pid > 0 {
					if p, e := os.FindProcess(pid); e == nil {
						_ = p.Kill()
					}
				}
				_ = manager.Stop("api", 100*time.Millisecond)
			})
			awaitExecution(t, ready)
			select {
			case pid = <-childPID:
			case <-time.After(time.Second):
				t.Fatal("missing child pid")
			}
			snapshot, ok := manager.Ownership("api")
			if !ok || snapshot.Stage != "pre_start" || snapshot.Command != pre || snapshot.Dir != dir || snapshot.PID <= 0 {
				t.Fatalf("ownership=%+v ok=%v", snapshot, ok)
			}
			if !manager.IsRunning("api") || !manager.IsAlive("api") || len(manager.List()) != 1 {
				t.Fatal("preparing owner absent from public queries")
			}
			if child, ok := manager.GetProcess("api"); !ok || child.Cmd == nil {
				t.Fatal("preparation misclassified as reconnect")
			}
			if err := manager.Reconnect("api", snapshot.PID, snapshot.PGID); err == nil {
				t.Error("Reconnect accepted duplicate preparation")
			}
			if err := manager.Start(ctx, "api", dir, executionCommand("main"), env, nil, 8); err == nil {
				t.Error("duplicate Start accepted")
			}
			if action == "cancel" {
				cancel()
			} else if action == "stop_all" {
				manager.StopAll(100 * time.Millisecond)
			} else if err := manager.Stop("api", 100*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-started:
				if err == nil {
					t.Fatal("canceled preparation succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("preparation did not retire")
			}
			if manager.IsRunning("api") || manager.IsAlive("api") || len(manager.List()) != 0 {
				t.Error("retired execution remains visible")
			}
			if _, _, ok := manager.GetProcessInfo("api"); ok {
				t.Error("retired PID visible")
			}
			if mainCalls.Load() != 0 || exitCalls.Load() != 0 {
				t.Error("preparation emitted main callback")
			}
			// Windows reports dead processes immediately; Unix can briefly retain a
			// reparented zombie, so the exact group/tree is also covered by Unix tests.
			if platform.IsProcessAlive(pid) {
				deadline := time.Now().Add(time.Second)
				for platform.IsProcessAlive(pid) && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if platform.IsProcessAlive(pid) {
					t.Error("preparation descendant survived retirement")
				}
			}
			nextCtx, nextCancel := context.WithCancel(context.Background())
			defer nextCancel()
			if err := manager.Start(nextCtx, "api", dir, executionCommand("main"), env, nil, 8); err != nil {
				t.Fatal("replacement:", err)
			}
			if err := manager.Stop("api", 100*time.Millisecond); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExecutionReservationBetweenChildrenHasNoFakePID(t *testing.T) {
	manager := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	gate := filepath.Join(dir, "spawn")
	_ = os.WriteFile(gate, nil, 0600)
	reserved := make(chan struct{})
	advance := make(chan struct{})
	between := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	manager.OnAction = func(_ string, msg string) {
		if strings.HasPrefix(msg, "running pre_start:") {
			once.Do(func() { close(reserved); <-advance })
		}
	}
	manager.OnOutput = func(_ string, line string) {
		if line == "[pre_start] exit 0" {
			close(between)
			<-release
		}
	}
	env := map[string]string{"ORBIT_EXECUTION_HELPER": "1", "ORBIT_EXECUTION_GATE": gate}
	done := make(chan error, 1)
	go func() {
		done <- manager.Start(ctx, "api", dir, executionCommand("main"), env, []string{executionCommand("quick")}, 1)
	}()
	var advanceOnce, releaseOnce sync.Once
	unblock := func() { advanceOnce.Do(func() { close(advance) }); releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() { cancel(); unblock(); _ = manager.Stop("api", 100*time.Millisecond) })
	assertReserved := func() {
		t.Helper()
		if !manager.IsRunning("api") || len(manager.List()) != 1 || manager.IsAlive("api") {
			t.Error("reservation queries inconsistent")
		}
		if _, _, ok := manager.GetProcessInfo("api"); ok {
			t.Error("reservation exposes fake PID")
		}
		if _, ok := manager.GetProcess("api"); ok {
			t.Error("reservation exposes fake process")
		}
	}
	awaitExecution(t, reserved)
	assertReserved()
	advanceOnce.Do(func() { close(advance) })
	awaitExecution(t, between)
	assertReserved()
	cancel()
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err == nil {
			t.Error("main launched after between-stage cancel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled boundary did not retire")
	}
}

func TestWindowsPreparationJobRetiresTreeOnOwnerCrash(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows job kill-on-close contract")
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExecutionProcessHelper$", "-test.timeout=15s", "--", "owner_crash")
	cmd.Env = append(os.Environ(), "ORBIT_EXECUTION_HELPER=1", "ORBIT_EXECUTION_DIR="+dir, "ORBIT_EXECUTION_GATE="+filepath.Join(dir, "spawn"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("owner helper: %v %s", err, out)
	}
	pid := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "CHILD-PID=") {
			pid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "CHILD-PID=")))
		}
	}
	if pid <= 0 {
		t.Fatalf("missing preparation descendant: %s", out)
	}
	t.Cleanup(func() {
		if p, e := os.FindProcess(pid); e == nil {
			_ = p.Kill()
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for platform.IsProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if platform.IsProcessAlive(pid) {
		t.Fatal("preparation descendant survived owner Job Object close")
	}
}

func TestImmediateSuccessfulPreparationIsNotAssignmentFailure(t *testing.T) {
	manager := NewManager()
	dir := t.TempDir()
	gate := filepath.Join(dir, "ready")
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"ORBIT_EXECUTION_HELPER": "1", "ORBIT_EXECUTION_GATE": gate}
	for i := 0; i < 10; i++ {
		if err := manager.Start(context.Background(), "quick", dir, executionCommand("main"), env, []string{executionCommand("instant")}, i); err != nil {
			t.Fatalf("short preparation %d: %v", i, err)
		}
		if err := manager.Stop("quick", 100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResumeFailureReturnsWithoutCrashingOrPublishingMain(t *testing.T) {
	for _, mode := range []string{"resume_failure", "cancel_before_resume"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExecutionProcessHelper$", "-test.timeout=8s", "--", mode)
			cmd.Env = append(os.Environ(), "ORBIT_EXECUTION_HELPER=1", "ORBIT_EXECUTION_DIR="+t.TempDir())
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("resume failure crashed owner: %v %s", err, out)
			}
		})
	}
}

func TestFastNaturalMainExitIsNotCallerCancellation(t *testing.T) {
	manager := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exits := make(chan int, 1)
	manager.OnExit = func(_ string, epoch int, err error, _ []string) {
		if err != nil {
			t.Errorf("natural main exit: %v", err)
		}
		exits <- epoch
	}
	env := map[string]string{"ORBIT_EXECUTION_HELPER": "1", "GORACE": "atexit_sleep_ms=0"}
	for epoch := 1; epoch <= 12; epoch++ {
		if err := manager.Start(ctx, "fast", t.TempDir(), executionCommand("instant"), env, nil, epoch); err != nil {
			t.Fatalf("successful dispatch became cancellation: %v", err)
		}
		select {
		case got := <-exits:
			if got != epoch {
				t.Fatalf("exit epoch=%d want%d", got, epoch)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("main exit missing")
		}
		if ctx.Err() != nil {
			t.Fatal("caller was canceled by natural retirement")
		}
		if manager.IsRunning("fast") {
			t.Fatal("natural owner did not retire")
		}
	}
	select {
	case extra := <-exits:
		t.Fatalf("duplicate exit=%d", extra)
	default:
	}
}
