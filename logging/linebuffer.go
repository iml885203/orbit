package logging

import (
	"bytes"
	"sync"
)

// LineBuffer is an io.Writer that accumulates bytes until a newline is seen,
// then emits complete lines via the configured callback. It is safe for a
// single writer goroutine; callers with multiple concurrent writers should
// use one LineBuffer per source.
//
// Partial trailing content is retained across writes. Callers MUST invoke
// Flush when the stream ends (EOF / process exit) to emit any final line
// that lacked a trailing newline.
type LineBuffer struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	emit func(line string)
}

// NewLineBuffer creates a LineBuffer that calls emit for every complete line.
// Lines are passed WITHOUT the trailing newline; a trailing \r (CRLF) is
// also stripped. A nil emit is treated as discard.
func NewLineBuffer(emit func(line string)) *LineBuffer {
	if emit == nil {
		emit = func(string) {}
	}
	return &LineBuffer{emit: emit}
}

// Write implements io.Writer. Always returns len(p), nil — callers streaming
// from a pipe or demuxer rely on this contract.
//
// Complete lines are extracted under the lock, then the lock is released
// before invoking emit, so a slow subscriber cannot stall concurrent writers
// or deadlock when emit re-enters the logging stack.
func (l *LineBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.buf.Write(p)
	lines := l.drainLocked()
	l.mu.Unlock()

	for _, line := range lines {
		l.emit(line)
	}
	return len(p), nil
}

// Flush emits any buffered bytes as a final line. Call on EOF.
func (l *LineBuffer) Flush() {
	l.mu.Lock()
	var trailing string
	if l.buf.Len() > 0 {
		trailing = cleanLine(l.buf.String())
		l.buf.Reset()
	}
	l.mu.Unlock()

	if trailing != "" {
		l.emit(trailing)
	}
}

// drainLocked extracts all complete lines currently in the buffer. Caller
// must hold l.mu. Returned lines are already CRLF-stripped; empty lines
// are dropped.
func (l *LineBuffer) drainLocked() []string {
	var out []string
	for {
		data := l.buf.Bytes()
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		line := cleanLine(string(data[:i]))
		l.buf.Next(i + 1)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func cleanLine(line string) string {
	if n := len(line); n > 0 && line[n-1] == '\r' {
		line = line[:n-1]
	}
	return line
}
