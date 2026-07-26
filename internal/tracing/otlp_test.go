package tracing

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/protobuf/proto"

	collectorpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func strAttr(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

func resourceSpans(service string, spans ...*tracepb.Span) *tracepb.ResourceSpans {
	return &tracepb.ResourceSpans{
		Resource:   &resourcepb.Resource{Attributes: []*commonpb.KeyValue{strAttr("service.name", service)}},
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}},
	}
}

// TestOTLPReceiverEndToEnd posts a real protobuf ExportTraceServiceRequest to
// the receiver handler and asserts the trace is queryable with the correct
// service path, nesting, and error status — the Phase 1 verification, minus
// the live daemon.
func TestOTLPReceiverEndToEnd(t *testing.T) {
	store := NewStore(100)
	srv := httptest.NewServer(store.OTLPHandler())
	defer srv.Close()

	traceID := []byte{0x0a, 0x1b, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	rootID := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	childID := []byte{2, 2, 2, 2, 2, 2, 2, 2}

	req := &collectorpb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{
			resourceSpans("api", &tracepb.Span{
				TraceId: traceID, SpanId: rootID, ParentSpanId: nil,
				Name: "POST /api/payment", Kind: tracepb.Span_SPAN_KIND_SERVER,
				StartTimeUnixNano: 1_000_000_000, EndTimeUnixNano: 1_412_000_000,
				Status: &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK},
			}),
			resourceSpans("billing", &tracepb.Span{
				TraceId: traceID, SpanId: childID, ParentSpanId: rootID,
				Name: "POST /settle", Kind: tracepb.Span_SPAN_KIND_SERVER,
				StartTimeUnixNano: 1_100_000_000, EndTimeUnixNano: 1_400_000_000,
				Status: &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "boom"},
			}),
		},
	}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp, err := http.Post(srv.URL+"/v1/traces", "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	list := store.List(0)
	if len(list) != 1 {
		t.Fatalf("want 1 trace, got %d", len(list))
	}
	sum := list[0]
	if sum.RootService != "api" || sum.RootName != "POST /api/payment" {
		t.Errorf("root = %s %q, want api POST /api/payment", sum.RootService, sum.RootName)
	}
	if sum.Status != "error" {
		t.Errorf("status = %s, want error", sum.Status)
	}
	if len(sum.Services) != 2 || sum.Services[0] != "api" || sum.Services[1] != "billing" {
		t.Errorf("services = %v, want [api billing]", sum.Services)
	}
	if sum.DurationMs != 412 {
		t.Errorf("duration = %vms, want 412", sum.DurationMs)
	}

	full, ok := store.Get(sum.TraceID)
	if !ok {
		t.Fatal("Get full trace failed")
	}
	if len(full.Spans) != 2 {
		t.Fatalf("want 2 spans, got %d", len(full.Spans))
	}
	// child must reference root as parent (nesting preserved)
	var child Span
	for _, s := range full.Spans {
		if s.Service == "billing" {
			child = s
		}
	}
	if child.ParentID != full.Spans[0].SpanID {
		t.Errorf("child parent = %s, want root span id %s", child.ParentID, full.Spans[0].SpanID)
	}
	if child.Status != "error" || child.StatusMsg != "boom" {
		t.Errorf("child status = %s/%q, want error/boom", child.Status, child.StatusMsg)
	}
}

// TestOTLPReceiverDropsMetricsAndLogs verifies the metrics/logs endpoints
// accept and discard payloads so a service's exporter doesn't error.
func TestOTLPReceiverDropsMetricsAndLogs(t *testing.T) {
	srv := httptest.NewServer(NewStore(10).OTLPHandler())
	defer srv.Close()
	for _, path := range []string{"/v1/metrics", "/v1/logs"} {
		resp, err := http.Post(srv.URL+path, "application/x-protobuf", bytes.NewReader([]byte{1, 2, 3}))
		if err != nil {
			t.Fatalf("post %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, resp.StatusCode)
		}
	}
}
