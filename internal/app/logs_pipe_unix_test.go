//go:build !windows

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/cli"
	"golang.org/x/sys/unix"
)

func TestLogsFollowInterruptsBlockedOutputAndClosesSubscriptions(t *testing.T) {
	var started sync.WaitGroup
	started.Add(2)
	var closed sync.WaitGroup
	closed.Add(2)
	prepareLogsCommand(t, func(w http.ResponseWriter, r *http.Request) {
		defer closed.Done()
		w.Header().Set("Content-Type", "text/event-stream")
		started.Done()
		started.Wait()
		for i := 0; i < 1000; i++ {
			if _, err := fmt.Fprintf(w, "data: %s\n\n", strings.Repeat("x", 1000)); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
	})
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLogsBlockedOutputChild$")
	child.Env = append(os.Environ(), "ORBIT_LOGS_BLOCKED_CHILD=1", "ORBIT_LOGS_BLOCKED_CONFIG="+configFile)
	child.Stdout = writer
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	for {
		descriptors := []unix.PollFd{{Fd: int32(writer.Fd()), Events: unix.POLLOUT}}
		if _, err := unix.Poll(descriptors, 0); err != nil && !errors.Is(err, syscall.EINTR) {
			t.Fatal(err)
		}
		if descriptors[0].Revents == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("child did not fill its output pipe")
		case <-time.After(time.Millisecond):
		}
	}
	if err := signalTestProcess(child); err != nil {
		t.Fatal(err)
	}
	err = child.Wait()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.Sys().(syscall.WaitStatus).Signal() != syscall.SIGINT {
		t.Fatalf("first Ctrl-C did not terminate blocked child: %v", err)
	}
	subscriptionsClosed := make(chan struct{})
	go func() { closed.Wait(); close(subscriptionsClosed) }()
	select {
	case <-subscriptionsClosed:
	case <-ctx.Done():
		t.Fatal("blocked-output subscriptions remained open")
	}
}

func TestLogsBlockedOutputChild(t *testing.T) {
	if os.Getenv("ORBIT_LOGS_BLOCKED_CHILD") != "1" {
		return
	}
	configFile = os.Getenv("ORBIT_LOGS_BLOCKED_CONFIG")
	cli.JSONOutput = true
	cmd := logsCmd()
	cmd.SetArgs([]string{"api", "web", "--follow"})
	if err := cmd.Execute(); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
