//go:build !windows

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const graceHelperMode = "ORBIT_PROCESS_GRACE_TEST"

func TestGraceProcessHelper(t *testing.T) {
	mode := os.Getenv(graceHelperMode)
	if mode == "" {
		return
	}
	if mode == "inherited_output" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGraceProcessHelper$", "-test.timeout=15s")
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, graceHelperMode+"=") {
				cmd.Env = append(cmd.Env, value)
			}
		}
		descendantMode := os.Getenv("ORBIT_PROCESS_INHERITED_MODE")
		if descendantMode == "" {
			descendantMode = "uncooperative"
		}
		readyReader, readyWriter, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer readyReader.Close()
		defer readyWriter.Close()
		cmd.ExtraFiles = []*os.File{readyWriter}
		cmd.Env = append(cmd.Env, graceHelperMode+"="+descendantMode, "ORBIT_PROCESS_READY_FD=1")
		var explicitStop chan os.Signal
		if os.Getenv("ORBIT_PROCESS_ESCAPE_GROUP") == "1" {
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			explicitStop = make(chan os.Signal, 1)
			signal.Notify(explicitStop, syscall.SIGTERM)
			defer signal.Stop(explicitStop)
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		if explicitStop != nil {
			fmt.Printf("OWNED-DESCENDANT=%d\n", cmd.Process.Pid)
		}
		published := false
		defer func() {
			if explicitStop != nil && !published {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}()
		_ = readyWriter.Close()
		var ready [1]byte
		if _, err := readyReader.Read(ready[:]); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("DESCENDANT=%d\n", cmd.Process.Pid)
		published = true
		if explicitStop != nil {
			<-explicitStop
			fmt.Println("LEADER-STOP-RECEIVED")
		}
		return
	}
	if mode == "uncooperative" {
		signal.Ignore(syscall.SIGTERM)
		graceHelperReady()
		fmt.Println("READY")
		time.Sleep(time.Hour)
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)
	graceHelperReady()
	fmt.Println("READY")
	<-signals
	if mode == "cleanup_gate" {
		gate := os.Getenv("ORBIT_PROCESS_CLEANUP_GATE")
		if err := os.WriteFile(gate+".ready", nil, 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(gate); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("cleanup gate did not open")
			}
			time.Sleep(time.Millisecond)
		}
	}
	time.Sleep(80 * time.Millisecond)
	if gate := os.Getenv("ORBIT_PROCESS_DRAIN_GATE"); gate != "" {
		fmt.Println("HANDLER")
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(gate); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("drain gate did not open")
			}
			time.Sleep(time.Millisecond)
		}
	}
	fmt.Println("FINAL-OUT")
	fmt.Fprint(os.Stdout, "unterminated stdout")
	fmt.Fprint(os.Stderr, "FINAL-ERR")
}

type graceExit struct {
	err    error
	stderr []string
}

func graceHelperCommand() string {
	return strconv.Quote(os.Args[0]) + " -test.run=^TestGraceProcessHelper$ -test.timeout=15s"
}

func graceHelperEnvironment(mode string) map[string]string {
	return map[string]string{graceHelperMode: mode, "GORACE": "atexit_sleep_ms=0"}
}

func TestLifecycleCancellationPreservesCooperativeGraceAndFinalOutput(t *testing.T) {
	for _, mode := range []string{"explicit", "cancel", "cancel_and_stop"} {
		t.Run(mode, func(t *testing.T) {
			manager := NewManager()
			manager.CancelGrace = 3 * time.Second
			ready := make(chan struct{}, 1)
			exited := make(chan graceExit, 1)
			var mu sync.Mutex
			var lines []string
			manager.OnOutput = func(_ string, line string) {
				mu.Lock()
				lines = append(lines, line)
				mu.Unlock()
				if line == "READY" {
					select {
					case ready <- struct{}{}:
					default:
					}
				}
			}
			manager.OnExit = func(_ string, _ int, err error, stderr []string) { exited <- graceExit{err, stderr} }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer func() { _ = manager.Stop("grace", 100*time.Millisecond) }()
			if err := manager.Start(ctx, "grace", t.TempDir(), graceHelperCommand(), graceHelperEnvironment("cooperative"), nil, 1); err != nil {
				t.Fatal(err)
			}
			select {
			case <-ready:
			case <-time.After(5 * time.Second):
				t.Fatal("helper did not become ready")
			}
			switch mode {
			case "explicit":
				if err := manager.Stop("grace", 3*time.Second); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			case "cancel_and_stop":
				cancel()
				results := make(chan error, 8)
				for range 8 {
					go func() { results <- manager.Stop("grace", 3*time.Second) }()
				}
				for range 8 {
					if err := <-results; err != nil {
						t.Fatal(err)
					}
				}
			}
			var exit graceExit
			select {
			case exit = <-exited:
			case <-time.After(5 * time.Second):
				t.Fatal("owned process was not reaped")
			}
			mu.Lock()
			output := append([]string(nil), lines...)
			mu.Unlock()
			if exit.err != nil || !containsExactLine(output, "FINAL-OUT") || !containsExactLine(output, "FINAL-ERR") || !containsExactLine(exit.stderr, "FINAL-ERR") {
				t.Fatalf("cooperative stop lost handler completion/output: error=%v output=%q stderr=%q", exit.err, output, exit.stderr)
			}
			if manager.IsRunning("grace") {
				t.Fatal("exited generation still tracked")
			}
		})
	}
}

func containsExactLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func TestCancellationDuringOwnershipRegistrationStillReapsProcess(t *testing.T) {
	manager := NewManager()
	manager.CancelGrace = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.OnStarted = func(string) { cancel() }
	exited := make(chan struct{}, 1)
	manager.OnExit = func(string, int, error, []string) { exited <- struct{}{} }
	err := manager.Start(ctx, "mid-launch", t.TempDir(), graceHelperCommand(), graceHelperEnvironment("uncooperative"), nil, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("start error=%v", err)
	}
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("mid-launch cancellation lost reaping/exit bookkeeping")
	}
	if manager.IsRunning("mid-launch") {
		t.Fatal("canceled generation still tracked")
	}
}

func TestUncooperativeCancellationStillEscalatesWithinGracePolicy(t *testing.T) {
	manager := NewManager()
	manager.CancelGrace = 150 * time.Millisecond
	ready := make(chan struct{}, 1)
	exited := make(chan graceExit, 1)
	manager.OnOutput = func(_ string, line string) {
		if line == "READY" {
			ready <- struct{}{}
		}
	}
	manager.OnExit = func(_ string, _ int, err error, stderr []string) { exited <- graceExit{err, stderr} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() { _ = manager.Stop("uncooperative", 100*time.Millisecond) }()
	if err := manager.Start(ctx, "uncooperative", t.TempDir(), graceHelperCommand(), graceHelperEnvironment("uncooperative"), nil, 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not become ready")
	}
	cancel()
	select {
	case exit := <-exited:
		var processErr *exec.ExitError
		if !errors.As(exit.err, &processErr) {
			t.Fatalf("unexpected exit classification: %v", exit.err)
		}
		status, ok := processErr.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatalf("uncooperative group was not escalated: %v", exit.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("uncooperative process outlived bounded escalation")
	}
	if manager.IsRunning("uncooperative") {
		t.Fatal("escalated generation still tracked")
	}
}

func TestPreStartCancellationDoesNotLaunchService(t *testing.T) {
	manager := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := false
	manager.OnStarted = func(string) { started = true }
	manager.OnOutput = func(_ string, line string) {
		if strings.TrimSpace(line) == "READY" {
			cancel()
		}
	}
	err := manager.Start(ctx, "pre-cancel", t.TempDir(), graceHelperCommand(), graceHelperEnvironment("uncooperative"), []string{graceHelperCommand()}, 1)
	if err == nil || ctx.Err() == nil || started || manager.IsRunning("pre-cancel") {
		t.Fatalf("pre-start cancellation launched service: error=%v started=%v", err, started)
	}
}

func graceHelperReady() {
	if os.Getenv("ORBIT_PROCESS_READY_FD") == "1" {
		file := os.NewFile(3, "helper-ready")
		_, _ = file.Write([]byte{1})
		_ = file.Close()
	}
}
