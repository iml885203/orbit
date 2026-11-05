package app

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fatih/color"
	"github.com/iml885203/orbit/internal/tracing"
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

func TestPrintTraceRowAlignsWithHeaderAndKeepsFullRoot(t *testing.T) {
	color.NoColor = true
	var buf bytes.Buffer
	printTraceHeader(&buf)
	const route = "GET api/customer/bet/{customerId:int}/sportsBetList-very-long-route"
	long := tracing.TraceSummary{
		TraceID: "0a1b", RootService: "worker", RootName: route,
		DurationMs: 1080, SpanCount: 3,
		Services: []string{"worker", "api", "billing", "catalog", "payments"},
		Status:   "error",
	}
	short := tracing.TraceSummary{
		TraceID: "0a1c", RootService: "api", RootName: "GET /x",
		DurationMs: 5, Services: []string{"api"}, Status: "ok",
	}
	printTraceRow(&buf, &long)
	printTraceRow(&buf, &short)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %d", len(lines))
	}
	// The span identity is never clipped.
	if !strings.HasSuffix(lines[1], "worker "+route) {
		t.Errorf("root should be printed in full: %q", lines[1])
	}
	// Services overflow is ellipsis-truncated, and the "…" / "→" multi-byte
	// runes must not shift the ROOT column: it starts at the same rune offset
	// on every line.
	if !strings.Contains(lines[1], "…") || !strings.Contains(lines[1], "ERR") {
		t.Errorf("long row: %q", lines[1])
	}
	rootCol := func(line, root string) int {
		before, _, found := strings.Cut(line, root)
		if !found {
			t.Fatalf("%q not in %q", root, line)
		}
		return utf8.RuneCountInString(before)
	}
	want := rootCol(lines[0], "ROOT")
	if got := rootCol(lines[1], "worker "+route); got != want {
		t.Errorf("long row ROOT at rune %d, header at %d", got, want)
	}
	if got := rootCol(lines[2], "api GET /x"); got != want {
		t.Errorf("short row ROOT at rune %d, header at %d", got, want)
	}
}

func TestPrintTraceListReportsHiddenHealthTraces(t *testing.T) {
	color.NoColor = true
	sum := tracing.TraceSummary{TraceID: "a", RootService: "api", RootName: "GET /x", Status: "ok"}

	var buf bytes.Buffer
	printTraceList(&buf, []tracing.TraceSummary{sum}, 7)
	if !strings.Contains(buf.String(), "7 health-probe trace(s) hidden (--include-health to show)") {
		t.Errorf("hidden count missing:\n%s", buf.String())
	}

	buf.Reset()
	printTraceList(&buf, nil, 3)
	out := buf.String()
	if strings.Contains(out, "ROOT") || !strings.Contains(out, "0 trace(s). 3 health-probe trace(s) hidden") {
		t.Errorf("all-hidden list should say so without an empty table:\n%s", out)
	}

	buf.Reset()
	printTraceList(&buf, []tracing.TraceSummary{sum}, 0)
	if strings.Contains(buf.String(), "hidden") {
		t.Errorf("nothing hidden should not mention it:\n%s", buf.String())
	}
}

func TestRenderWaterfallKeepsLongLabelsAligned(t *testing.T) {
	color.NoColor = true
	tr := fixtureTrace()
	tr.Spans[1].Service = "catalog-api"
	tr.Spans[1].Name = "GET /api/catalog/items/1"
	var buf bytes.Buffer
	renderWaterfall(&buf, tr)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if !strings.HasSuffix(lines[2], "  catalog-api GET /api/catalog/items/1") {
		t.Errorf("label clipped: %q", lines[2])
	}
	// Bars start at the line start on every span row, regardless of label.
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "│") {
			t.Errorf("bar not at column 0: %q", l)
		}
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
	if got := truncate("a…bcd", 3); got != "a……" {
		t.Errorf("truncate counts runes: %q", got)
	}
	if got := truncate("abc", 4); got != "abc" {
		t.Errorf("truncate short = %q", got)
	}
}
