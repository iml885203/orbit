package logging

import "sync"

// Multiplexer captures log output from multiple services and fans out
// to ring buffers and optional subscribers.
type Multiplexer struct {
	buffers     map[string]*RingBuffer
	subscribers map[int]func(service string, line string)
	nextID      int
	mu          sync.RWMutex
}

// NewMultiplexer creates a new log multiplexer.
func NewMultiplexer() *Multiplexer {
	return &Multiplexer{
		buffers:     make(map[string]*RingBuffer),
		subscribers: make(map[int]func(service string, line string)),
	}
}

// Subscribe adds a callback that receives all log lines.
// Returns an unsubscribe function that removes the callback.
func (m *Multiplexer) Subscribe(fn func(service string, line string)) func() {
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	m.subscribers[id] = fn
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		delete(m.subscribers, id)
		m.mu.Unlock()
	}
}

// SubscriberCount reports how many active subscribers exist. Intended for
// tests that verify proper unsubscribe bookkeeping.
func (m *Multiplexer) SubscriberCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.subscribers)
}

// Write records a single log line from a service. Thread-safe.
//
// Callers MUST pass one complete line per call, without a trailing newline.
// Producers that read from pipes or demuxers should feed their bytes through
// a LineBuffer before invoking Write.
func (m *Multiplexer) Write(service string, line string) {
	if line == "" {
		return
	}
	m.mu.Lock()
	buf, ok := m.buffers[service]
	if !ok {
		buf = NewRingBuffer(defaultBufferSize)
		m.buffers[service] = buf
	}
	subs := make([]func(string, string), 0, len(m.subscribers))
	for _, fn := range m.subscribers {
		subs = append(subs, fn)
	}
	m.mu.Unlock()

	buf.Write(line)
	for _, fn := range subs {
		fn(service, line)
	}
}

// GetBuffer returns the ring buffer for a service.
func (m *Multiplexer) GetBuffer(service string) *RingBuffer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.buffers[service]
}

// GetLastLines returns the last N lines for a service.
func (m *Multiplexer) GetLastLines(service string, n int) []string {
	buf := m.GetBuffer(service)
	if buf == nil {
		return nil
	}
	return buf.Last(n)
}

// Services returns the names of all services with log data.
func (m *Multiplexer) Services() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.buffers))
	for name := range m.buffers {
		names = append(names, name)
	}
	return names
}
