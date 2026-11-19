package logging

import (
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	defaultBufferSize   = 1000
	maxLogLineBytes     = 64 << 10
	maxServiceLogBytes  = 1 << 20
	truncatedLineMarker = " [line truncated]"
)

type RingRetention struct {
	RetainedBytes  int
	EvictedLines   uint64
	EvictedBytes   uint64
	TruncatedLines uint64
	TruncatedBytes uint64
}

type RingBuffer struct {
	lines     []string
	size      int
	head      int
	count     int
	retention RingRetention
	mu        sync.RWMutex
}

func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = defaultBufferSize
	}
	size = min(size, defaultBufferSize)
	return &RingBuffer{lines: make([]string, size), size: size}
}

func (r *RingBuffer) Write(line string) { r.write(line) }

func retainedLogLine(line string) (string, int) {
	if len(line) <= maxLogLineBytes ||
		(len(line) <= maxLogLineBytes+len(continuedLineMarker) && strings.HasSuffix(line, continuedLineMarker)) {
		return strings.Clone(line), 0
	}
	cut := maxLogLineBytes
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut] + truncatedLineMarker, len(line) - cut
}

func (r *RingBuffer) write(line string) string {
	line, dropped := retainedLogLine(line)
	r.mu.Lock()
	defer r.mu.Unlock()
	if dropped > 0 {
		r.retention.TruncatedLines++
		r.retention.TruncatedBytes += uint64(dropped)
	}
	for r.count > 0 && (r.count == r.size || r.retention.RetainedBytes+len(line) > maxServiceLogBytes) {
		r.evictOldest()
	}
	r.lines[r.head] = line
	r.head = (r.head + 1) % r.size
	r.count++
	r.retention.RetainedBytes += len(line)
	return line
}

func (r *RingBuffer) evictOldest() {
	index := (r.head - r.count + r.size) % r.size
	removed := len(r.lines[index])
	r.lines[index] = ""
	r.count--
	r.retention.RetainedBytes -= removed
	r.retention.EvictedLines++
	r.retention.EvictedBytes += uint64(removed)
}

func (r *RingBuffer) Lines() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.linesLocked()
}

func (r *RingBuffer) linesLocked() []string {
	result := make([]string, 0, r.count)
	start := (r.head - r.count + r.size) % r.size
	for i := 0; i < r.count; i++ {
		result = append(result, r.lines[(start+i)%r.size])
	}
	return result
}

func (r *RingBuffer) Last(n int) []string {
	all := r.Lines()
	n = max(n, 0)
	if n >= len(all) {
		return all
	}
	return all[len(all)-n:]
}

func (r *RingBuffer) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}

func (r *RingBuffer) Retention() RingRetention {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.retention
}

func (r *RingBuffer) snapshot() *RingBuffer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clone := NewRingBuffer(r.size)
	copy(clone.lines, r.linesLocked())
	clone.count = r.count
	clone.head = r.count % r.size
	clone.retention = r.retention
	return clone
}
