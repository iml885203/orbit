package tracing

import (
	"sort"
	"sync"
	"time"
)

// Store is the in-memory trace ring buffer. It accumulates spans by trace,
// evicts the oldest trace once capacity is exceeded, and notifies subscribers
// when a trace changes.
//
// All mutable state is guarded by mu. Subscriber channels use non-blocking
// send (drop-on-full): trace summaries are observational, and a slow UI that
// misses an intermediate update still gets the trace on its next poll. See
// .claude/rules/go-event-loop.md.
type Store struct {
	mu      sync.RWMutex
	max     int
	order   []string               // traceIDs in first-seen order (oldest first)
	byID    map[string]*traceAccum // traceID -> accumulating spans
	subs    map[int]chan TraceSummary
	nextSub int

	totalSpans   int64
	spansDropped int64
	lastReceived time.Time
	recentSpans  []time.Time // span receive times within the rate window

	// Receiver health, written once by the daemon after it attempts to bind
	// the OTLP listener (see Store.SetReceiver). Guarded by mu like the rest.
	// The store owns the data; the daemon owns the listener — this field is
	// how the listener's real bind outcome reaches Stats() (and thus the CLI
	// and dashboard) without the store importing net/http.
	receiverHealthy bool
	receiverPort    int    // port actually bound; 0 when unhealthy
	receiverErr     string // bind failure reason, when any
}

type traceAccum struct {
	firstSeen time.Time
	spans     map[string]Span // spanID -> span (last write wins; dedupes re-exports)
}

const rateWindow = time.Minute

// Ingest ceilings. always_on sampling (the local-dev default) means a busy
// service can push unbounded spans into a single trace and unbounded attribute
// bytes per span; without caps the in-memory store grows with traffic, not with
// the trace-count limit. These bound the two axes MaxTraces does not:
//   - maxSpansPerTrace caps fan-out within one trace (a hot loop emitting
//     thousands of child spans). Spans beyond the cap are dropped and counted.
//   - maxAttrBytesPerSpan caps the stringified attribute payload per span
//     (a span carrying a full SQL statement or request body). Oversized
//     attribute sets are dropped whole (the span is kept, its attributes are
//     nil) so one giant span can't dominate memory.
//
// The OTLP request body ceiling lives in otlp.go (maxOTLPBodyBytes).
const (
	maxSpansPerTrace    = 2000
	maxAttrBytesPerSpan = 16 * 1024
)

// NewStore returns a store holding at most max traces (min 1).
func NewStore(max int) *Store {
	if max < 1 {
		max = 1
	}
	return &Store{
		max:  max,
		byID: make(map[string]*traceAccum),
		subs: make(map[int]chan TraceSummary),
	}
}

// Ingest adds spans to the store and notifies subscribers of every affected
// trace. Spans may arrive across multiple OTLP exports and out of order.
func (s *Store) Ingest(spans []Span) {
	if len(spans) == 0 {
		return
	}
	now := time.Now()

	s.mu.Lock()
	touched := make(map[string]struct{})
	accepted := 0
	for _, sp := range spans {
		if sp.TraceID == "" || sp.SpanID == "" {
			continue
		}
		acc, ok := s.byID[sp.TraceID]
		if !ok {
			acc = &traceAccum{firstSeen: now, spans: make(map[string]Span)}
			s.byID[sp.TraceID] = acc
			s.order = append(s.order, sp.TraceID)
		}
		// Per-trace span cap: drop new spans once a trace is saturated, but
		// still allow updates to spans already stored (re-exports of the same
		// span id must not be rejected as "new").
		if _, exists := acc.spans[sp.SpanID]; !exists && len(acc.spans) >= maxSpansPerTrace {
			s.spansDropped++
			continue
		}
		// Per-span attribute-bytes cap: drop the attribute set whole rather
		// than the span, so the span still shows in the waterfall.
		if attrBytes(sp.Attributes) > maxAttrBytesPerSpan {
			sp.Attributes = nil
		}
		acc.spans[sp.SpanID] = sp
		touched[sp.TraceID] = struct{}{}
		accepted++
	}
	s.totalSpans += int64(accepted)
	s.lastReceived = now
	s.recordRate(now, accepted)
	s.evictLocked()

	// Build summaries for touched traces that survived eviction, then notify
	// outside the spans loop so each trace is sent at most once.
	summaries := make([]TraceSummary, 0, len(touched))
	for id := range touched {
		if acc, ok := s.byID[id]; ok {
			summaries = append(summaries, summarize(id, acc))
		}
	}
	s.mu.Unlock()

	s.broadcast(summaries)
}

// attrBytes sums the key and value lengths of a span's attribute map — the
// measure the per-span attribute ceiling is checked against. Approximate (it
// ignores map overhead), which is fine for a guardrail.
func attrBytes(attrs map[string]string) int {
	n := 0
	for k, v := range attrs {
		n += len(k) + len(v)
	}
	return n
}

// recordRate appends span receive times and prunes outside the rate window.
// Caller holds mu.
func (s *Store) recordRate(now time.Time, n int) {
	cutoff := now.Add(-rateWindow)
	for i := 0; i < n; i++ {
		s.recentSpans = append(s.recentSpans, now)
	}
	keep := 0
	for _, t := range s.recentSpans {
		if t.After(cutoff) {
			s.recentSpans[keep] = t
			keep++
		}
	}
	s.recentSpans = s.recentSpans[:keep]
}

// evictLocked drops oldest traces until within capacity. Caller holds mu.
func (s *Store) evictLocked() {
	for len(s.order) > s.max {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.byID, oldest)
	}
}

// List returns up to limit trace summaries, newest first. limit <= 0 returns all.
func (s *Store) List(limit int) []TraceSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TraceSummary, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, summarize(id, s.byID[id]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartUnixNano > out[j].StartUnixNano })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Get returns the full trace for id, or ok=false if it is unknown or evicted.
func (s *Store) Get(id string) (Trace, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	acc, ok := s.byID[id]
	if !ok {
		return Trace{}, false
	}
	spans := sortedSpans(acc)
	sum := summarizeSorted(id, acc, spans)
	return Trace{
		TraceID:       sum.TraceID,
		RootService:   sum.RootService,
		RootName:      sum.RootName,
		StartUnixNano: sum.StartUnixNano,
		DurationMs:    sum.DurationMs,
		SpanCount:     sum.SpanCount,
		Services:      sum.Services,
		Status:        sum.Status,
		Spans:         spans,
	}, true
}

// sortedSpans copies a trace's span set into a slice ordered by
// (StartUnixNano, SpanID). The two-key comparator is the ordering invariant
// the whole feature relies on (waterfall layout, summary derivation, CLI
// rendering) — it lives here and nowhere else.
func sortedSpans(acc *traceAccum) []Span {
	spans := make([]Span, 0, len(acc.spans))
	for _, sp := range acc.spans {
		spans = append(spans, sp)
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].StartUnixNano != spans[j].StartUnixNano {
			return spans[i].StartUnixNano < spans[j].StartUnixNano
		}
		return spans[i].SpanID < spans[j].SpanID
	})
	return spans
}

// SetReceiver records the OTLP listener's real bind outcome. The daemon calls
// this exactly once after attempting to bind: healthy=true with the bound port
// on success, or healthy=false with the failure reason (and port 0) when the
// bind failed or tracing is disabled. It is the sole writer of the receiver-
// health fields, so Stats() reports the listener's actual state rather than
// mere config intent.
func (s *Store) SetReceiver(healthy bool, port int, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receiverHealthy = healthy
	if healthy {
		s.receiverPort = port
	} else {
		s.receiverPort = 0
	}
	s.receiverErr = errMsg
}

// Stats returns the collector health snapshot. Configured and OTLPPort are
// filled by the caller (they come from config, not the store); the store owns
// the receiver-health, counter, and rate fields.
func (s *Store) Stats() TracingStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var lastMs int64
	if !s.lastReceived.IsZero() {
		lastMs = s.lastReceived.UnixMilli()
	}
	return TracingStatus{
		ReceiverHealthy:    s.receiverHealthy,
		ActualPort:         s.receiverPort,
		ReceiverError:      s.receiverErr,
		TraceCount:         len(s.order),
		TotalSpans:         s.totalSpans,
		SpansPerMin:        len(s.recentSpans),
		LastReceivedUnixMs: lastMs,
		SpansDropped:       s.spansDropped,
	}
}

// Subscribe returns a channel of trace-summary updates and an unsubscribe
// func. Non-blocking send; slow subscribers drop updates.
func (s *Store) Subscribe() (<-chan TraceSummary, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSub
	s.nextSub++
	ch := make(chan TraceSummary, 256)
	s.subs[id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if c, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(c)
		}
	}
}

// broadcast sends each summary to every subscriber under a single read lock.
// Non-blocking sends; observational subscribers that are too slow drop frames.
func (s *Store) broadcast(sums []TraceSummary) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.subs {
		for _, sum := range sums {
			select {
			case ch <- sum:
			default: // observational subscriber too slow; drop
			}
		}
	}
}

// summarize derives a TraceSummary from accumulated spans. Spans are walked in
// start-time order so Services (first-seen) and the root (earliest parentless
// span) are deterministic regardless of map iteration order. start/duration
// span the full set; status is error if any span errored.
func summarize(id string, acc *traceAccum) TraceSummary {
	return summarizeSorted(id, acc, sortedSpans(acc))
}

// summarizeSorted is summarize for callers that already hold the sorted span
// slice (Get builds it for Trace.Spans anyway — no second sort).
func summarizeSorted(id string, acc *traceAccum, spans []Span) TraceSummary {
	sum := TraceSummary{TraceID: id, SpanCount: len(acc.spans), Status: "ok"}
	if len(spans) == 0 {
		return sum
	}

	// Spans are sorted ascending by start, so the earliest start is spans[0];
	// only maxEnd needs the loop (end times aren't sorted).
	minStart := spans[0].StartUnixNano
	maxEnd := minStart
	seenSvc := make(map[string]bool)
	haveRoot := false

	for i := range spans {
		sp := &spans[i]
		if sp.EndUnixNano > maxEnd {
			maxEnd = sp.EndUnixNano
		}
		if sp.Service != "" && !seenSvc[sp.Service] {
			seenSvc[sp.Service] = true
			sum.Services = append(sum.Services, sp.Service)
		}
		if sp.Status == "error" {
			sum.Status = "error"
		}
		_, parentInSet := acc.spans[sp.ParentID]
		isRoot := sp.ParentID == "" || !parentInSet
		if isRoot && !haveRoot {
			haveRoot = true
			sum.RootService = sp.Service
			sum.RootName = sp.Name
		}
	}
	sum.StartUnixNano = minStart
	sum.DurationMs = float64(maxEnd-minStart) / 1e6
	return sum
}
