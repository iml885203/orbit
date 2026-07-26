package logging

import "sync"

const defaultBufferSize = 1000

// RingBuffer is a fixed-size circular buffer for log lines.
type RingBuffer struct {
	lines []string
	size  int
	head  int
	count int
	mu    sync.RWMutex
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = defaultBufferSize
	}
	return &RingBuffer{
		lines: make([]string, size),
		size:  size,
	}
}

// Write adds a line to the buffer.
func (r *RingBuffer) Write(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines[r.head] = line
	r.head = (r.head + 1) % r.size
	if r.count < r.size {
		r.count++
	}
}

// Lines returns all buffered lines in order (oldest first).
func (r *RingBuffer) Lines() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]string, 0, r.count)
	if r.count < r.size {
		result = append(result, r.lines[:r.count]...)
	} else {
		result = append(result, r.lines[r.head:]...)
		result = append(result, r.lines[:r.head]...)
	}
	return result
}

// Last returns the last n lines.
func (r *RingBuffer) Last(n int) []string {
	all := r.Lines()
	if n >= len(all) {
		return all
	}
	return all[len(all)-n:]
}

// Count returns the number of lines in the buffer.
func (r *RingBuffer) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}
