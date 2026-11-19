package tracing

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func assertTraceAccounting(t *testing.T, s *Store) {
	t.Helper()
	total := 0
	for id, acc := range s.byID {
		want := traceOverhead + len(id)
		for id := range acc.spans {
			want += retainedSpanBytes(acc.spans[id])
		}
		if acc.bytes != want {
			t.Fatalf("trace %s bytes=%d want%d", id, acc.bytes, want)
		}
		total += want
	}
	if s.retainedBytes != total || total > s.maxBytes {
		t.Fatalf("store bytes=%d actual%d cap%d", s.retainedBytes, total, s.maxBytes)
	}
}

func TestTraceByteBudgetUpdatesEvictionAndFreshStore(t *testing.T) {
	s := NewStore(100)
	s.maxBytes = 4000
	for i := 0; i < 50; i++ {
		name := strings.Repeat("x", 100+(i%4)*300)
		s.Ingest([]Span{{TraceID: fmt.Sprint(i / 3), SpanID: fmt.Sprint(i % 2), Name: name}})
		assertTraceAccounting(t, s)
	}
	if s.tracesEvicted == 0 {
		t.Fatal("expected byte eviction below trace count limit")
	}
	last := s.order[len(s.order)-1]
	before := s.retainedBytes
	span := s.byID[last].spans["0"]
	if span.SpanID == "" {
		span = s.byID[last].spans["1"]
	}
	s.Ingest([]Span{span, span})
	if s.retainedBytes != before {
		t.Fatal("duplicates grew retained bytes")
	}
	assertTraceAccounting(t, s)
	if _, ok := s.Get("0"); ok {
		t.Fatal("evicted trace remains queryable")
	}
	fresh := NewStore(100)
	if fresh.Stats().RetainedBytes != 0 || fresh.Stats().TracesEvicted != 0 {
		t.Fatal("fresh store retained counters")
	}
}

func TestTraceStringsAndAttributeOwnership(t *testing.T) {
	s := NewStore(10)
	attrs := map[string]string{"http.route": "/health"}
	s.Ingest([]Span{{TraceID: "t", SpanID: "s", Service: "api", Name: strings.Repeat("界", 10000), StatusMsg: strings.Repeat("界", 10000), Attributes: attrs}})
	attrs["http.route"] = strings.Repeat("x", 1<<20)
	got, ok := s.Get("t")
	if !ok {
		t.Fatal("trace absent")
	}
	for _, v := range []string{got.Spans[0].Name, got.Spans[0].StatusMsg} {
		if !utf8.ValidString(v) || !strings.HasSuffix(v, truncatedMark) || len(v) > maxLogValueBytes+len(truncatedMark) {
			t.Fatal("unsafe/unmarked truncation")
		}
	}
	if got.Spans[0].Attributes["http.route"] != "/health" {
		t.Fatal("caller mutated retained attributes")
	}
	got.Spans[0].Attributes["http.route"] = "mutated query"
	again, _ := s.Get("t")
	if again.Spans[0].Attributes["http.route"] != "/health" {
		t.Fatal("query mutated retained attributes")
	}
	for _, field := range []string{"trace", "span", "parent", "service"} {
		sp := Span{TraceID: "bad", SpanID: "bad"}
		huge := strings.Repeat("a", maxTraceIdentityBytes+1)
		switch field {
		case "trace":
			sp.TraceID = huge
		case "span":
			sp.SpanID = huge
		case "parent":
			sp.ParentID = huge
		case "service":
			sp.Service = huge
		}
		s.Ingest([]Span{sp})
	}
	if s.Stats().SpansDropped != 4 || s.Stats().TraceCount != 1 {
		t.Fatal("oversize identities weren't rejected")
	}
	assertTraceAccounting(t, s)
}

func TestTraceRateBookkeepingStaysBoundedAndExpires(t *testing.T) {
	s := NewStore(1)
	now := time.Unix(1000, 0)
	for i := 0; i < 100000; i++ {
		s.recordRate(now, 1)
	}
	if got := s.spansPerMinute(now); got != 100000 {
		t.Fatalf("rate=%d", got)
	}
	if s.spansPerMinute(now.Add(time.Minute)) != 0 {
		t.Fatal("idle rate didn't expire")
	}
	s.recordRate(now.Add(time.Minute), 2)
	if s.spansPerMinute(now.Add(time.Minute)) != 2 {
		t.Fatal("old bucket survived reuse")
	}
}

func TestLogNonAttributeBoundsAndOwnership(t *testing.T) {
	s := NewLogStore(100, 1<<20)
	attrs := map[string]string{"key": "value"}
	s.Ingest([]LogRecord{{Service: "api", SeverityText: strings.Repeat("界", 10000), Scope: strings.Repeat("界", 10000), Attributes: attrs}})
	attrs["key"] = "changed"
	page := s.Query(0, LogFilter{})
	if len(page.Logs) != 1 {
		t.Fatal("normal record missing")
	}
	record := page.Logs[0]
	for _, value := range []string{record.SeverityText, record.Scope} {
		if !utf8.ValidString(value) || !strings.HasSuffix(value, truncatedMark) || len(value) > maxLogValueBytes+len(truncatedMark) {
			t.Fatal("nonattribute value unbounded")
		}
	}
	if record.Attributes["key"] != "value" {
		t.Fatal("input map retained")
	}
	record.Attributes["key"] = "query mutation"
	if s.Query(0, LogFilter{}).Logs[0].Attributes["key"] != "value" {
		t.Fatal("query map retained")
	}
	s.Ingest([]LogRecord{{Service: strings.Repeat("a", maxTraceIdentityBytes+1)}})
	if len(s.Query(0, LogFilter{}).Logs) != 1 {
		t.Fatal("oversize service was cropped into an identity")
	}
}
