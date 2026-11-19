package logging

import (
	"strings"
	"sync"
)

const (
	maxMultiplexerBytes    = 16 << 20
	maxLogServices         = 128
	maxLogServiceNameBytes = 256
)

type serviceLogs struct {
	buffer    *RingBuffer
	lastWrite uint64
}

type LogRetention struct {
	RetainedBytes        int
	Services             int
	EvictedServices      uint64
	EvictedLines         uint64
	EvictedBytes         uint64
	TruncatedLines       uint64
	TruncatedBytes       uint64
	RejectedServiceLines uint64
}

// Multiplexer retains bounded service histories. Subscribers run synchronously
// after retention is unlocked; subscribers that enqueue observations must own
// their queue limits and drop policy.
type Multiplexer struct {
	buffers       map[string]*serviceLogs
	subscribers   map[int]func(service string, line string)
	nextID        int
	writeSequence uint64
	retention     LogRetention
	mu            sync.RWMutex
}

func NewMultiplexer() *Multiplexer {
	return &Multiplexer{buffers: make(map[string]*serviceLogs), subscribers: make(map[int]func(string, string))}
}

func (m *Multiplexer) Subscribe(fn func(service string, line string)) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.subscribeLocked(fn)
}

// Follow snapshots history and registers its subscriber under the same lock
// used by Write, so a line appears in backlog or live output, never both.
func (m *Multiplexer) Follow(service string, n int, fn func(line string)) (backlog []string, unsubscribe func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if logs := m.buffers[service]; logs != nil && n > 0 {
		backlog = logs.buffer.Last(n)
	}
	return backlog, m.subscribeLocked(func(svc, line string) {
		if svc == service {
			fn(line)
		}
	})
}

func (m *Multiplexer) subscribeLocked(fn func(service string, line string)) func() {
	id := m.nextID
	m.nextID++
	m.subscribers[id] = fn
	return func() { m.mu.Lock(); delete(m.subscribers, id); m.mu.Unlock() }
}

func (m *Multiplexer) SubscriberCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.subscribers)
}

func (m *Multiplexer) Write(service string, line string) {
	if line == "" {
		return
	}
	m.mu.Lock()
	if len(service) > maxLogServiceNameBytes {
		m.retention.RejectedServiceLines++
		m.mu.Unlock()
		return
	}
	service = strings.Clone(service)
	logs := m.buffers[service]
	if logs == nil {
		if len(m.buffers) == maxLogServices {
			m.evictOldestService()
		}
		logs = &serviceLogs{buffer: NewRingBuffer(defaultBufferSize)}
		m.buffers[service] = logs
	}
	before := logs.buffer.Retention()
	line = logs.buffer.write(line)
	after := logs.buffer.Retention()
	m.retention.RetainedBytes += after.RetainedBytes - before.RetainedBytes
	m.retention.EvictedLines += after.EvictedLines - before.EvictedLines
	m.retention.EvictedBytes += after.EvictedBytes - before.EvictedBytes
	m.retention.TruncatedLines += after.TruncatedLines - before.TruncatedLines
	m.retention.TruncatedBytes += after.TruncatedBytes - before.TruncatedBytes
	m.writeSequence++
	logs.lastWrite = m.writeSequence
	for m.retention.RetainedBytes > maxMultiplexerBytes {
		m.evictOldestService()
	}
	subs := make([]func(string, string), 0, len(m.subscribers))
	for _, fn := range m.subscribers {
		subs = append(subs, fn)
	}
	m.mu.Unlock()
	for _, fn := range subs {
		fn(service, line)
	}
}

func (m *Multiplexer) evictOldestService() {
	var oldest string
	sequence := ^uint64(0)
	for service, logs := range m.buffers {
		if logs.lastWrite < sequence {
			oldest = service
			sequence = logs.lastWrite
		}
	}
	logs := m.buffers[oldest]
	stats := logs.buffer.Retention()
	m.retention.RetainedBytes -= stats.RetainedBytes
	m.retention.EvictedServices++
	m.retention.EvictedLines += uint64(logs.buffer.Count())
	m.retention.EvictedBytes += uint64(stats.RetainedBytes)
	delete(m.buffers, oldest)
}

// GetBuffer returns an independent snapshot; retained history can only be
// mutated through Write so aggregate accounting cannot be bypassed.
func (m *Multiplexer) GetBuffer(service string) *RingBuffer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if logs := m.buffers[service]; logs != nil {
		return logs.buffer.snapshot()
	}
	return nil
}

func (m *Multiplexer) GetLastLines(service string, n int) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if logs := m.buffers[service]; logs != nil {
		return logs.buffer.Last(n)
	}
	return nil
}

func (m *Multiplexer) Services() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.buffers))
	for name := range m.buffers {
		names = append(names, name)
	}
	return names
}

func (m *Multiplexer) Retention() LogRetention {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stats := m.retention
	stats.Services = len(m.buffers)
	return stats
}

func (m *Multiplexer) HasLogs(service string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	logs := m.buffers[service]
	return logs != nil && logs.buffer.Count() > 0
}

func (m *Multiplexer) GetLines(service string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if logs := m.buffers[service]; logs != nil {
		return logs.buffer.Lines()
	}
	return nil
}
