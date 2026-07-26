package tracing

import (
	"strconv"
	"testing"
)

func sp(trace, span, parent, service, name string, start, end int64, status string) Span {
	return Span{
		TraceID: trace, SpanID: span, ParentID: parent, Service: service, Name: name,
		StartUnixNano: start, EndUnixNano: end, DurationMs: float64(end-start) / 1e6, Status: status,
	}
}

func TestSummarizeRootServiceAndDuration(t *testing.T) {
	s := NewStore(10)
	// root api 0..400ms, child billing 50..330ms (error), child kafka 100..112ms
	s.Ingest([]Span{
		sp("t1", "a", "", "api", "POST /api/payment", 0, 400_000_000, "ok"),
		sp("t1", "b", "a", "billing", "POST /settle", 50_000_000, 330_000_000, "error"),
		sp("t1", "c", "a", "api", "kafka produce", 100_000_000, 112_000_000, "ok"),
	})

	list := s.List(0)
	if len(list) != 1 {
		t.Fatalf("want 1 trace, got %d", len(list))
	}
	g := list[0]
	if g.RootService != "api" || g.RootName != "POST /api/payment" {
		t.Errorf("root = %s %q, want api POST /api/payment", g.RootService, g.RootName)
	}
	if g.SpanCount != 3 {
		t.Errorf("spanCount = %d, want 3", g.SpanCount)
	}
	if g.Status != "error" {
		t.Errorf("status = %s, want error (a child errored)", g.Status)
	}
	if g.DurationMs != 400 {
		t.Errorf("duration = %vms, want 400 (min start to max end)", g.DurationMs)
	}
	// services in first-seen order, api then billing (kafka span is api too)
	if len(g.Services) != 2 || g.Services[0] != "api" || g.Services[1] != "billing" {
		t.Errorf("services = %v, want [api billing]", g.Services)
	}
}

func TestGetReturnsSortedSpansAndNotFound(t *testing.T) {
	s := NewStore(10)
	s.Ingest([]Span{
		sp("t1", "b", "a", "billing", "child", 50, 80, "ok"),
		sp("t1", "a", "", "api", "root", 0, 100, "ok"),
	})
	tr, ok := s.Get("t1")
	if !ok {
		t.Fatal("Get(t1) not found")
	}
	if len(tr.Spans) != 2 || tr.Spans[0].SpanID != "a" {
		t.Errorf("spans not sorted by start; got %+v", tr.Spans)
	}
	if _, ok := s.Get("nope"); ok {
		t.Error("Get(nope) should be not found")
	}
}

func TestRingBufferEvictsOldest(t *testing.T) {
	s := NewStore(2)
	s.Ingest([]Span{sp("t1", "a", "", "api", "r", 10, 20, "ok")})
	s.Ingest([]Span{sp("t2", "a", "", "api", "r", 20, 30, "ok")})
	s.Ingest([]Span{sp("t3", "a", "", "api", "r", 30, 40, "ok")})

	if _, ok := s.Get("t1"); ok {
		t.Error("t1 should have been evicted")
	}
	if _, ok := s.Get("t3"); !ok {
		t.Error("t3 should be present")
	}
	if c := len(s.List(0)); c != 2 {
		t.Errorf("trace count = %d, want 2", c)
	}
}

func TestReExportDedupesBySpanID(t *testing.T) {
	s := NewStore(10)
	s.Ingest([]Span{sp("t1", "a", "", "api", "r", 0, 100, "ok")})
	// same span id arrives again (re-export) — must not double-count
	s.Ingest([]Span{sp("t1", "a", "", "api", "r", 0, 100, "ok")})
	tr, _ := s.Get("t1")
	if tr.SpanCount != 1 {
		t.Errorf("spanCount = %d, want 1 (dedup by span id)", tr.SpanCount)
	}
}

func TestStatsCounts(t *testing.T) {
	s := NewStore(10)
	s.Ingest([]Span{
		sp("t1", "a", "", "api", "r", 0, 100, "ok"),
		sp("t1", "b", "a", "billing", "c", 10, 90, "ok"),
	})
	st := s.Stats()
	if st.TotalSpans != 2 || st.TraceCount != 1 {
		t.Errorf("stats = %+v, want TotalSpans=2 TraceCount=1", st)
	}
	if st.SpansPerMin != 2 {
		t.Errorf("spansPerMin = %d, want 2", st.SpansPerMin)
	}
	if st.LastReceivedUnixMs == 0 {
		t.Error("lastReceived should be set after ingest")
	}
}

func TestSubscribeReceivesSummary(t *testing.T) {
	s := NewStore(10)
	ch, cancel := s.Subscribe()
	defer cancel()
	s.Ingest([]Span{sp("t1", "a", "", "api", "r", 0, 100, "error")})
	select {
	case sum := <-ch:
		if sum.TraceID != "t1" || sum.Status != "error" {
			t.Errorf("got %+v, want t1/error", sum)
		}
	default:
		t.Fatal("expected a summary on the subscription channel")
	}
}

func TestIngestCapsSpansPerTrace(t *testing.T) {
	s := NewStore(10)
	// Push more spans into one trace than the per-trace cap allows.
	over := maxSpansPerTrace + 50
	batch := make([]Span, 0, over)
	for i := 0; i < over; i++ {
		batch = append(batch, sp("t1", spanID(i), "", "api", "r", int64(i), int64(i+1), "ok"))
	}
	s.Ingest(batch)

	tr, ok := s.Get("t1")
	if !ok {
		t.Fatal("trace missing")
	}
	if tr.SpanCount != maxSpansPerTrace {
		t.Errorf("SpanCount = %d, want cap %d", tr.SpanCount, maxSpansPerTrace)
	}
	if dropped := s.Stats().SpansDropped; dropped != int64(over-maxSpansPerTrace) {
		t.Errorf("SpansDropped = %d, want %d", dropped, over-maxSpansPerTrace)
	}
}

func TestIngestDropsOversizedAttributes(t *testing.T) {
	s := NewStore(10)
	big := sp("t1", "a", "", "api", "r", 0, 10, "ok")
	huge := make([]byte, maxAttrBytesPerSpan+1)
	for i := range huge {
		huge[i] = 'x'
	}
	big.Attributes = map[string]string{"sql": string(huge)}
	s.Ingest([]Span{big})

	tr, ok := s.Get("t1")
	if !ok || len(tr.Spans) != 1 {
		t.Fatal("span should be kept even when its attributes are dropped")
	}
	if tr.Spans[0].Attributes != nil {
		t.Errorf("oversized attributes should be dropped, got %v", tr.Spans[0].Attributes)
	}
}

func TestSetReceiverReflectedInStats(t *testing.T) {
	s := NewStore(10)
	// Unhealthy: no port, carries the error.
	s.SetReceiver(false, 0, "bind: address already in use")
	st := s.Stats()
	if st.ReceiverHealthy || st.ActualPort != 0 || st.ReceiverError == "" {
		t.Errorf("unhealthy stats wrong: %+v", st)
	}
	// Healthy: reports the bound port, clears the error.
	s.SetReceiver(true, 4319, "")
	st = s.Stats()
	if !st.ReceiverHealthy || st.ActualPort != 4319 || st.ReceiverError != "" {
		t.Errorf("healthy stats wrong: %+v", st)
	}
}

func spanID(i int) string {
	return "s" + strconv.Itoa(i)
}
