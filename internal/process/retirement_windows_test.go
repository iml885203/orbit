//go:build windows

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/platform"
	"golang.org/x/sys/windows"
)

func TestWindowsIndependentOutputHelper(t *testing.T) {
	if os.Getenv("ORBIT_WIN_RETIRE_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "main" {
		time.Sleep(15 * time.Second)
		return
	}
	if mode == "child" {
		if err := os.WriteFile(os.Getenv("ORBIT_WIN_RETIRE_READY"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Second)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsIndependentOutputHelper$", "-test.timeout=20s", "--", "child")
	cmd.Env = os.Environ()
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	cmd.Stdout, cmd.Stderr = null, null
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("ORBIT_WIN_RETIRE_READY")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("descendant not ready")
		}
		time.Sleep(time.Millisecond)
	}
	fmt.Printf("DESCENDANT=%d\n", cmd.Process.Pid)
	deadline = time.Now().Add(10 * time.Second)
	for {
		if mode == "prepare" {
			if _, err := os.Stat(os.Getenv("ORBIT_WIN_RETIRE_EXIT")); err == nil {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("test owner not stopped")
		}
		time.Sleep(time.Millisecond)
	}
}

func windowsIndependentCommand(stage string) string {
	return strconv.Quote(os.Args[0]) + " -test.run=^TestWindowsIndependentOutputHelper$ -test.timeout=20s -- " + stage
}

func TestWindowsRetirementSuccessMeansDescendantHandleSignaledAtReturn(t *testing.T) {
	for _, action := range []string{"stop", "natural_preparation"} {
		t.Run(action, func(t *testing.T) {
			manager := NewManager()
			manager.CancelGrace = 100 * time.Millisecond
			dir := t.TempDir()
			gate := filepath.Join(dir, "exit")
			ready := filepath.Join(dir, "ready")
			environment := map[string]string{"ORBIT_WIN_RETIRE_HELPER": "1", "ORBIT_WIN_RETIRE_READY": ready, "ORBIT_WIN_RETIRE_EXIT": gate}
			descendants := make(chan int, 1)
			var mainCalls atomic.Int32
			manager.OnStarted = func(string) { mainCalls.Add(1) }
			manager.OnOutput = func(_ string, line string) {
				if strings.HasPrefix(line, "DESCENDANT=") {
					pid, _ := strconv.Atoi(strings.TrimPrefix(line, "DESCENDANT="))
					descendants <- pid
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			start := make(chan error, 1)
			stage := "hold"
			if action == "natural_preparation" {
				stage = "prepare"
			}
			go func() {
				start <- manager.Start(ctx, "api", dir, windowsIndependentCommand("main"), environment, []string{windowsIndependentCommand(stage)}, 1)
			}()
			pid := 0
			t.Cleanup(func() {
				cancel()
				_ = manager.Stop("api", 100*time.Millisecond)
				if pid > 0 {
					if p, err := os.FindProcess(pid); err == nil {
						_ = p.Kill()
					}
				}
			})
			select {
			case pid = <-descendants:
			case <-time.After(5 * time.Second):
				t.Fatal("descendant not published")
			}
			handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
			if err != nil {
				t.Fatal(err)
			}
			closed := false
			defer func() {
				if !closed {
					_ = windows.CloseHandle(handle)
				}
			}()
			var result error
			if action == "stop" {
				result = manager.Stop("api", 100*time.Millisecond)
			} else {
				if err := os.WriteFile(gate, nil, 0600); err != nil {
					t.Fatal(err)
				}
				select {
				case result = <-start:
				case <-time.After(6 * time.Second):
					t.Fatal("preparation retirement did not return")
				}
			}
			signal, waitErr := windows.WaitForSingleObject(handle, 0)
			if waitErr != nil {
				t.Fatal(waitErr)
			}
			if result == nil {
				if signal != windows.WAIT_OBJECT_0 {
					t.Fatalf("SUCCESS returned before descendant terminated: wait=%d", signal)
				}
			} else {
				if !errors.Is(result, platform.ErrJobRetirementTimeout) {
					t.Fatalf("unexpected retirement error: %v", result)
				}
				if _, ok := manager.Ownership("api"); !ok || !manager.IsRunning("api") {
					t.Fatal("uncertainty erased recoverable owner")
				}
				if action == "natural_preparation" && mainCalls.Load() != 0 {
					t.Fatal("uncertain preparation launched main")
				}
			}
			// An external reference can keep accounting nonzero after the process is
			// signaled. Release that reference before requesting a bounded retry.
			_ = windows.CloseHandle(handle)
			closed = true
			if result != nil {
				if err := manager.Stop("api", 100*time.Millisecond); err != nil {
					t.Fatal("retry after releasing external reference:", err)
				}
			}
			if action == "stop" {
				select {
				case <-start:
				case <-time.After(5 * time.Second):
					t.Fatal("Start did not join retirement")
				}
			}
			if err := manager.Stop("api", 100*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if manager.IsRunning("api") {
				t.Fatal("retirement still blocks replacement")
			}
			if err := manager.Start(context.Background(), "api", dir, windowsIndependentCommand("main"), environment, nil, 2); err != nil {
				t.Fatal("replacement:", err)
			}
			if err := manager.Stop("api", 100*time.Millisecond); err != nil {
				t.Fatal(err)
			}
		})
	}
}
