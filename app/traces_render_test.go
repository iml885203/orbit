package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/tracing"
	"github.com/fatih/color"
)

func fixtureTrace() *tracing.Trace {
	return &tracing.Trace{
		TraceID:       "0a1b02030405060708090a0b0c0d0e0f",
		RootService:   "api",
		RootName:      "POST /api/payment",
		StartUnixNano: 1_000_000_000,
		DurationMs:    412,
		SpanCount:     2,
		Services:      []string{"api", "billing"},
		Status:        "error",
		Spans: []tracing.Span{
			{
				TraceID: "0a1b02030405060708090a0b0c0d0e0f", SpanID: "aaaa", Service: "api",
				Name: "POST /api/payment", Kind: "server",
				StartUnixNano: 1_000_000_000, EndUnixNano: 1_412_000_000, DurationMs: 412, Status: "ok",
			},
			{
				TraceID: "0a1b02030405060708090a0b0c0d0e0f", SpanID: "bbbb", ParentID: "aaaa", Service: "billing",
				Name: "POST /settle", Kind: "server",
				StartUnixNano: 1_100_000_000, EndUnixNano: 1_400_000_000, DurationMs: 300, Status: "error",
			},
		},
	}
}

func TestRenderWaterfall(t *testing.T) {
	color.NoColor = true // deterministic output regardless of TTY
	var buf bytes.Buffer
	renderWaterfall(&buf, fixtureTrace())
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 span lines, got %d:\n%s", len(lines), out)
	}

	if !strings.Contains(lines[0], "trace 0a1b0203040…") ||
		!strings.Contains(lines[0], "api POST /api/payment") ||
		!strings.Contains(lines[0], "412ms") ||
		!strings.Contains(lines[0], "2 spans") ||
		!strings.Contains(lines[0], "ERROR") {
		t.Errorf("header wrong: %q", lines[0])
	}

	// Root span: full-width bar, no error marker.
	if !strings.Contains(lines[1], "api POST /api/payment") || strings.Contains(lines[1], "✗") {
		t.Errorf("root line wrong: %q", lines[1])
	}
	// Child span: indented under the root, error-marked, offset bar (starts
	// with spaces inside the │…│ track because it begins 100ms in).
	if !strings.Contains(lines[2], "  billing POST /settle") || !strings.Contains(lines[2], "✗") {
		t.Errorf("child line wrong: %q", lines[2])
	}
	if !strings.Contains(lines[2], "│ ") {
		t.Errorf("child bar should be offset from trace start: %q", lines[2])
	}
}

func TestPrintTraceRowAlignsWithHeader(t *testing.T) {
	color.NoColor = true
	var buf bytes.Buffer
	printTraceHeader(&buf)
	sum := tracing.TraceSummary{
		TraceID:     "0a1b",
		RootService: "worker",
		RootName:    "GET api/customer/bet/{customerId:int}/sportsBetList-very-long-route",
		DurationMs:  1080, SpanCount: 3,
		Services: []string{"worker", "api", "billing", "catalog", "payments"},
		Status:   "error",
	}
	printTraceRow(&buf, &sum)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want header + row, got %d", len(lines))
	}
	// Column contract: header and row share traceRowFmt, so the DUR column
	// must start at the same rune offset in both lines.
	headerDur := strings.Index(lines[0], "DUR")
	rowDur := strings.Index(lines[1], "1.08s")
	if headerDur < 0 || rowDur < 0 {
		t.Fatalf("missing DUR/duration: header=%q row=%q", lines[0], lines[1])
	}
	// "%8s" right-aligns within the column; "DUR" is also inside that width.
	if rowDur > headerDur+8 || rowDur < headerDur-8 {
		t.Errorf("duration column misaligned: header DUR at %d, row value at %d", headerDur, rowDur)
	}
	// Overflowing cells are truncated with an ellipsis, never widen the column.
	if !strings.Contains(lines[1], "…") {
		t.Errorf("long root/services should be ellipsis-truncated: %q", lines[1])
	}
	if !strings.Contains(lines[1], "ERR") {
		t.Errorf("error status missing: %q", lines[1])
	}
}

func TestSpanDepthsCLI(t *testing.T) {
	spans := []tracing.Span{
		{SpanID: "a"},
		{SpanID: "b", ParentID: "a"},
		{SpanID: "c", ParentID: "b"},
		{SpanID: "orphan", ParentID: "missing"}, // absent parent = root depth
	}
	d := spanDepths(spans)
	for id, want := range map[string]int{"a": 0, "b": 1, "c": 2, "orphan": 0} {
		if d[id] != want {
			t.Errorf("depth[%s] = %d, want %d", id, d[id], want)
		}
	}
}

func TestFmtMsAndTruncate(t *testing.T) {
	if got := fmtMs(412); got != "412ms" {
		t.Errorf("fmtMs(412) = %q", got)
	}
	if got := fmtMs(1200); got != "1.20s" {
		t.Errorf("fmtMs(1200) = %q", got)
	}
	if got := truncate("abcdef", 4); got != "abc…" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("abc", 4); got != "abc" {
		t.Errorf("truncate short = %q", got)
	}
}
