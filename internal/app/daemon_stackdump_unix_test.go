//go:build !windows

package app

import (
	"bytes"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type dumpSink struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	written chan struct{}
}

func (s *dumpSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.buf.Write(p)
	if strings.Contains(s.buf.String(), "watchGoroutineDumpSignal") {
		select {
		case <-s.written:
		default:
			close(s.written)
		}
	}
	return n, err
}

func TestSIGUSR1WritesGoroutineDumpWithoutStoppingTheProcess(t *testing.T) {
	sink := &dumpSink{written: make(chan struct{})}
	stop := watchGoroutineDumpSignal(sink)
	defer stop()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("signal self: %v", err)
	}
	select {
	case <-sink.written:
	case <-time.After(5 * time.Second):
		t.Fatal("SIGUSR1 produced no goroutine dump")
	}

	sink.mu.Lock()
	dump := sink.buf.String()
	sink.mu.Unlock()
	if !strings.Contains(dump, "=== orbit daemon goroutine dump pid=") {
		t.Fatalf("dump lacks its header:\n%s", dump)
	}
	if !strings.Contains(dump, "goroutine ") {
		t.Fatalf("dump lacks goroutine stacks:\n%s", dump)
	}
}
