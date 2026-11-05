package logging

import (
	"strings"
	"sync"
	"testing"
)

func collectLines() (*LineBuffer, *[]string) {
	var mu sync.Mutex
	var out []string
	lb := NewLineBuffer(func(line string) {
		mu.Lock()
		out = append(out, line)
		mu.Unlock()
	})
	return lb, &out
}

func TestLineBuffer_CompleteLines(t *testing.T) {
	lb, out := collectLines()
	_, _ = lb.Write([]byte("hello\nworld\n"))
	if got := *out; len(got) != 2 || got[0] != "hello" || got[1] != "world" {
		t.Errorf("got %q", got)
	}
}

func TestLineBuffer_PartialThenComplete(t *testing.T) {
	lb, out := collectLines()
	_, _ = lb.Write([]byte("par"))
	_, _ = lb.Write([]byte("t\n"))
	if got := *out; len(got) != 1 || got[0] != "part" {
		t.Errorf("expected one line 'part', got %q", got)
	}
}

func TestLineBuffer_TrailingPartial_NeedsFlush(t *testing.T) {
	lb, out := collectLines()
	_, _ = lb.Write([]byte("no-newline"))
	if got := *out; len(got) != 0 {
		t.Errorf("expected no emission before flush, got %q", got)
	}
	lb.Flush()
	if got := *out; len(got) != 1 || got[0] != "no-newline" {
		t.Errorf("flush: got %q", got)
	}
}

func TestLineBuffer_CRLF(t *testing.T) {
	lb, out := collectLines()
	_, _ = lb.Write([]byte("win\r\nunix\n"))
	if got := *out; len(got) != 2 || got[0] != "win" || got[1] != "unix" {
		t.Errorf("got %q", got)
	}
}

func TestLineBuffer_EmptyLinesDropped(t *testing.T) {
	lb, out := collectLines()
	_, _ = lb.Write([]byte("\n\nkeep\n\n"))
	if got := *out; len(got) != 1 || got[0] != "keep" {
		t.Errorf("got %q", got)
	}
}

func TestLineBuffer_LongLineAcrossManyWrites(t *testing.T) {
	lb, out := collectLines()
	long := strings.Repeat("X", 10000)
	chunks := 50
	size := len(long) / chunks
	for i := 0; i < chunks; i++ {
		_, _ = lb.Write([]byte(long[i*size : (i+1)*size]))
	}
	_, _ = lb.Write([]byte("\n"))
	if got := *out; len(got) != 1 || got[0] != long {
		t.Errorf("long-line reassembly failed: got %d lines, first len=%d", len(got), len(got[0]))
	}
}

func TestLineBuffer_WriteReturnsFullLen(t *testing.T) {
	lb, _ := collectLines()
	n, err := lb.Write([]byte("partial"))
	if err != nil || n != 7 {
		t.Errorf("Write returned (%d, %v); want (7, nil)", n, err)
	}
}

func TestNewLineBuffer_NilEmitIsNoOp(t *testing.T) {
	lb := NewLineBuffer(nil)
	n, err := lb.Write([]byte("line1\nline2\n"))
	if err != nil {
		t.Fatalf("Write returned err: %v", err)
	}
	if n != 12 {
		t.Errorf("Write returned %d, want 12", n)
	}
	lb.Flush() // must not panic
}
