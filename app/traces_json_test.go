package app

import (
	"encoding/json"
	"testing"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/tracing"
)

// These tests pin the tracing wire contract that `orbit trace --json` /
// `orbit trace --json` expose to agents (docs/agent-cli.md). A renamed or
// dropped JSON key is a breaking change for every consumer — fail here first.

func jsonKeys(t *testing.T, v any) map[string]bool {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	keys := make(map[string]bool, len(m))
	for k := range m {
		keys[k] = true
	}
	return keys
}

func assertExactKeys(t *testing.T, got map[string]bool, want []string) {
	t.Helper()
	for _, k := range want {
		if !got[k] {
			t.Errorf("missing wire key %q", k)
		}
		delete(got, k)
	}
	for k := range got {
		t.Errorf("unexpected wire key %q — update docs/agent-cli.md and this contract test together", k)
	}
}

func TestTraceSummaryWireContract(t *testing.T) {
	sum := tracing.TraceSummary{
		TraceID: "t", RootService: "api", RootName: "POST /x",
		StartUnixNano: 1, DurationMs: 2, SpanCount: 3,
		Services: []string{"api"}, Status: "ok",
	}
	assertExactKeys(t, jsonKeys(t, sum), []string{
		"traceId", "rootService", "rootName", "startUnixNano",
		"durationMs", "spanCount", "services", "status",
	})
}

func TestTraceAndSpanWireContract(t *testing.T) {
	tr := tracing.Trace{
		TraceID: "t", RootService: "api", RootName: "POST /x",
		StartUnixNano: 1, DurationMs: 2, SpanCount: 1,
		Services: []string{"api"}, Status: "error",
		Spans: []tracing.Span{{
			TraceID: "t", SpanID: "s", ParentID: "p", Service: "api", Name: "op",
			Kind: "server", StartUnixNano: 1, EndUnixNano: 2, DurationMs: 1,
			Status: "error", StatusMsg: "boom", Attributes: map[string]string{"k": "v"},
		}},
	}
	assertExactKeys(t, jsonKeys(t, tr), []string{
		"traceId", "rootService", "rootName", "startUnixNano",
		"durationMs", "spanCount", "services", "status", "spans",
	})
	assertExactKeys(t, jsonKeys(t, tr.Spans[0]), []string{
		"traceId", "spanId", "parentId", "service", "name", "kind",
		"startUnixNano", "endUnixNano", "durationMs", "status", "statusMsg", "attributes",
	})
}

func TestTraceLogLineWireContract(t *testing.T) {
	assertExactKeys(t, jsonKeys(t, daemon.TraceLogLine{Service: "api", Line: "l"}),
		[]string{"service", "line"})
}
