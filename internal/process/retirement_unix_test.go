//go:build !windows

package process

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSuccessfulTreeRetryPreservesRecordedDrainTimeout(t *testing.T) {
	manager := NewManager()
	failure := errors.New("controlled cleanup failure with inherited output")
	var recovered atomic.Bool
	manager.terminateGroup = func(pgid int, grace time.Duration) error {
		if !recovered.Load() {
			return failure
		}
		return KillGroup(pgid, grace)
	}
	descendants := make(chan int, 1)
	manager.OnOutput = func(_ string, line string) {
		if strings.HasPrefix(line, "DESCENDANT=") {
			pid, _ := strconv.Atoi(strings.TrimPrefix(line, "DESCENDANT="))
			descendants <- pid
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	done := make(chan error, 1)
	go func() {
		done <- manager.Start(ctx, "api", dir, "sleep 30", graceHelperEnvironment("inherited_output"), []string{graceHelperCommand()}, 1)
	}()
	pid := 0
	t.Cleanup(func() {
		recovered.Store(true)
		cancel()
		if pid > 0 {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
		_ = manager.Stop("api", 0)
	})
	select {
	case pid = <-descendants:
	case <-time.After(5 * time.Second):
		t.Fatal("inherited output helper not ready")
	}
	select {
	case err := <-done:
		if !errors.Is(err, failure) || !errors.Is(err, ErrOutputDrainTimeout) {
			t.Fatalf("combined preparation error=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bounded drain did not return")
	}
	if _, ok := manager.Ownership("api"); !ok {
		t.Fatal("failed cleanup identity lost")
	}
	recovered.Store(true)
	if err := manager.Stop("api", 0); !errors.Is(err, ErrOutputDrainTimeout) {
		t.Fatalf("retry hid output loss: %v", err)
	}
	if manager.IsRunning("api") {
		t.Fatal("output diagnostic blocked retired owner")
	}
	if _, ok := manager.Ownership("api"); ok {
		t.Fatal("successful retry left old identity")
	}
	if err := manager.Start(context.Background(), "api", dir, "sleep 30", nil, nil, 2); err != nil {
		t.Fatal("replacement:", err)
	}
	if err := manager.Stop("api", 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
}
