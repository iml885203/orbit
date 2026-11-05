package extension

import (
	"context"
	"testing"
	"time"
)

// RunChannel is the seam every feature SSE source flows through — pin the
// forwardChan-parity behaviors: blocking emit, unsubscribe on ctx cancel,
// termination on upstream close, and stop-on-emit-false.
func TestRunChannel_ForwardsAndUnsubscribesOnCancel(t *testing.T) {
	ch := make(chan string, 1)
	cancelled := make(chan struct{})
	run := RunChannel(func() (<-chan string, func()) {
		return ch, func() { close(cancelled) }
	})

	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan any, 1)
	done := make(chan struct{})
	go func() {
		run(ctx, func(data any) bool { got <- data; return true })
		close(done)
	}()

	ch <- "frame"
	select {
	case v := <-got:
		if v != "frame" {
			t.Fatalf("forwarded %v, want frame", v)
		}
	case <-time.After(time.Second):
		t.Fatal("frame never forwarded")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return on ctx cancel")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("subscription cancel func never called")
	}
}

func TestRunChannel_StopsOnUpstreamCloseAndEmitFalse(t *testing.T) {
	// Upstream close terminates Run.
	closed := make(chan string)
	close(closed)
	run := RunChannel(func() (<-chan string, func()) { return closed, func() {} })
	done := make(chan struct{})
	go func() {
		run(context.Background(), func(any) bool { return true })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return on upstream close")
	}

	// emit=false (connection gone mid-send) stops the loop without
	// draining another value.
	ch := make(chan string, 2)
	ch <- "a"
	ch <- "b"
	run = RunChannel(func() (<-chan string, func()) { return ch, func() {} })
	done = make(chan struct{})
	go func() {
		run(context.Background(), func(any) bool { return false })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after emit returned false")
	}
	if len(ch) != 1 {
		t.Fatalf("loop drained %d extra values after emit=false, want b left in channel", 1-len(ch))
	}
}
