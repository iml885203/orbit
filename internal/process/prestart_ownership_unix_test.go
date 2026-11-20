//go:build !windows

package process

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// This draft uses grace_test.go's existing self-executed descendant helper.
// The exact published PID is retained so a broken baseline cannot leak it.
func TestPreStartTreeRetiresBeforeStopOrCancellationReturns(t *testing.T) {
	for _, mode := range []string{"cancel", "stop"} {
		t.Run(mode, func(t *testing.T) {
			manager := NewManager()
			manager.CancelGrace = 100 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			descendant := make(chan int, 1)
			var mu sync.Mutex
			mainStarted := false
			manager.OnOutput = func(_ string, line string) {
				if strings.HasPrefix(line, "DESCENDANT=") {
					pid, err := strconv.Atoi(strings.TrimPrefix(line, "DESCENDANT="))
					if err == nil {
						select {
						case descendant <- pid:
						default:
						}
					}
				}
				if line == "MAIN-STARTED" {
					mu.Lock()
					mainStarted = true
					mu.Unlock()
				}
			}
			main := writePreStartScript(t, "echo MAIN-STARTED\nexec sleep 30")
			started := make(chan error, 1)
			dir := t.TempDir()
			go func() {
				started <- manager.Start(ctx, "startup", dir, main,
					graceHelperEnvironment("inherited_output"), []string{graceHelperCommand()}, 42)
			}()
			pid := 0
			startObserved := false
			t.Cleanup(func() {
				cancel()
				if pid > 0 {
					process, err := os.FindProcess(pid)
					if err == nil {
						_ = process.Kill()
					}
				}
				if !startObserved {
					select {
					case <-started:
					case <-time.After(4 * time.Second):
						t.Error("Start did not retire after exact descendant cleanup")
					}
				}
				_ = manager.Stop("startup", 100*time.Millisecond)
			})
			select {
			case pid = <-descendant:
			case err := <-started:
				startObserved = true
				t.Fatalf("pre_start returned before descendant readiness: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("pre_start helper did not publish descendant")
			}
			if mode == "cancel" {
				cancel()
			} else {
				stopped := make(chan error, 1)
				go func() { stopped <- manager.Stop("startup", 100*time.Millisecond) }()
				select {
				case err := <-stopped:
					if err != nil {
						t.Errorf("Stop: %v", err)
					}
				case <-time.After(4 * time.Second):
					t.Error("Stop failed to join preparing execution within grace and drain budget")
				}
			}
			select {
			case err := <-started:
				startObserved = true
				if err == nil {
					t.Error("stopped pre_start must not continue to main")
				}
			case <-time.After(4 * time.Second):
				t.Error("pre_start Start still waits on descendant-inherited output after cancellation/Stop")
			}
			mu.Lock()
			launchedMain := mainStarted
			mu.Unlock()
			if launchedMain {
				t.Error("main child launched after cancellation/Stop")
			}
			if manager.IsRunning("startup") {
				t.Error("execution ownership remains after completed stop/cancellation")
			}
		})
	}
}
