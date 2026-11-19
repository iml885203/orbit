package logging

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDefaultLineBufferDrainsUnterminatedOutputInBoundedChunks(t *testing.T) {
	var lines []string
	buffer := NewLineBuffer(func(line string) { lines = append(lines, line) })
	input := strings.Repeat("界", (2<<20)/3)
	for start := 0; start < len(input); {
		end := min(start+8191, len(input))
		n, err := buffer.Write([]byte(input[start:end]))
		if err != nil || n != end-start {
			t.Fatalf("write=%d err=%v", n, err)
		}
		start = end
	}
	if buffer.buf.Len() > 64<<10+utf8.UTFMax {
		t.Fatalf("partial retained %d bytes", buffer.buf.Len())
	}
	buffer.Flush()
	if len(lines) < 2 {
		t.Fatalf("unterminated stream was retained as %d line", len(lines))
	}
	var restored strings.Builder
	for _, line := range lines {
		if !utf8.ValidString(line) || len(line) > 64<<10+len(" [line continues]") {
			t.Fatalf("invalid or unbounded chunk: %d", len(line))
		}
		restored.WriteString(strings.TrimSuffix(line, " [line continues]"))
	}
	if restored.String() != input {
		t.Fatal("continuation chunks lost or repeated input")
	}
}

func TestRingBoundsHugeLinesAndRetainedBytes(t *testing.T) {
	ring := NewRingBuffer(1000)
	for i := 0; i < 40; i++ {
		ring.Write(strings.Repeat("界", 30000))
	}
	total := 0
	for _, line := range ring.Lines() {
		total += len(line)
		if len(line) > 64<<10+len(" [line truncated]") || !utf8.ValidString(line) {
			t.Fatalf("unbounded line %d", len(line))
		}
	}
	if total > 1<<20 {
		t.Fatalf("retained %d bytes above service budget", total)
	}
}

func TestMultiplexerBoundsAggregateHistoryAndServiceMetadata(t *testing.T) {
	mux := NewMultiplexer()
	payload := strings.Repeat("x", 64<<10)
	for svc := 0; svc < 40; svc++ {
		for line := 0; line < 16; line++ {
			mux.Write(fmt.Sprint(svc), payload)
		}
	}
	total := 0
	for _, svc := range mux.Services() {
		for _, line := range mux.GetLastLines(svc, 1000) {
			total += len(line)
		}
	}
	if total > 16<<20 {
		t.Fatalf("aggregate history retained %d bytes", total)
	}
	for svc := 0; svc < 256; svc++ {
		mux.Write(fmt.Sprint(svc), "small")
	}
	if len(mux.Services()) > 128 {
		t.Fatalf("unbounded service metadata: %d", len(mux.Services()))
	}
	mux.Write(strings.Repeat("service", 1000), "line")
	for _, name := range mux.Services() {
		if len(name) > 256 {
			t.Fatalf("oversized service key retained: %d", len(name))
		}
	}
}

func TestMultiplexerReturnedBufferCannotBypassRetentionOwnership(t *testing.T) {
	mux := NewMultiplexer()
	mux.Write("api", "original")
	buffer := mux.GetBuffer("api")
	buffer.Write("external write")
	if got := mux.GetLastLines("api", 10); len(got) != 1 || got[0] != "original" {
		t.Fatalf("external mutation bypassed multiplexer: %q", got)
	}
}

func TestDefaultStreamContinuationSurvivesHistoryAndFollow(t *testing.T) {
	mux := NewMultiplexer()
	var live []string
	backlog, unsubscribe := mux.Follow("api", 100, func(line string) { live = append(live, line) })
	defer unsubscribe()
	if len(backlog) != 0 {
		t.Fatal("unexpected backlog")
	}
	writer := NewLineBuffer(func(line string) { mux.Write("api", line) })
	input := strings.Repeat("界", 100000)
	for start := 0; start < len(input); {
		end := min(start+8191, len(input))
		_, _ = writer.Write([]byte(input[start:end]))
		start = end
	}
	_, _ = writer.Write([]byte("\r\nlast partial"))
	writer.Flush()
	stored := mux.GetLastLines("api", 100)
	if len(stored) != len(live) {
		t.Fatalf("history=%d follow=%d", len(stored), len(live))
	}
	var restored strings.Builder
	for i, line := range stored {
		if line != live[i] || !utf8.ValidString(line) {
			t.Fatal("history/follow content diverged")
		}
		if i == len(stored)-1 {
			if line != "last partial" {
				t.Fatalf("lost final partial: %q", line)
			}
		} else {
			restored.WriteString(strings.TrimSuffix(line, continuedLineMarker))
		}
	}
	if restored.String() != input {
		t.Fatal("retained continuation lost or repeated stream content")
	}
	if stats := mux.Retention(); stats.TruncatedLines != 0 {
		t.Fatalf("continuation marker counted as lost payload: %+v", stats)
	}
}

func TestRetentionCountersDescribeLostHistoryWithoutPerServiceTombstones(t *testing.T) {
	mux := NewMultiplexer()
	for i := 0; i < 1005; i++ {
		mux.Write("api", "line")
	}
	stats := mux.Retention()
	if stats.EvictedLines != 5 || stats.EvictedBytes != 20 || stats.RetainedBytes != 4000 {
		t.Fatalf("count eviction: %+v", stats)
	}
	mux.Write("api", strings.Repeat("界", 30000))
	stats = mux.Retention()
	if stats.TruncatedLines != 1 || stats.TruncatedBytes == 0 {
		t.Fatalf("line truncation not visible: %+v", stats)
	}
	for i := 0; i < 256; i++ {
		mux.Write(fmt.Sprint(i), "small")
	}
	stats = mux.Retention()
	if stats.Services != 128 || stats.EvictedServices != 129 {
		t.Fatalf("service eviction: %+v", stats)
	}
	mux.Write(strings.Repeat("s", 257), "dropped")
	if stats = mux.Retention(); stats.RejectedServiceLines != 1 || stats.Services != 128 {
		t.Fatalf("oversized name retained: %+v", stats)
	}
	total := 0
	for _, name := range mux.Services() {
		for _, line := range mux.GetLastLines(name, 1000) {
			total += len(line)
		}
	}
	if total != stats.RetainedBytes {
		t.Fatalf("accounted=%d actual=%d", stats.RetainedBytes, total)
	}
}

func TestHugeDirectLineUsesSameBoundedValueForHistoryAndFollow(t *testing.T) {
	mux := NewMultiplexer()
	var live []string
	_, unsubscribe := mux.Follow("api", 0, func(line string) { live = append(live, line) })
	defer unsubscribe()
	mux.Write("api", strings.Repeat("界", 30000))
	stored := mux.GetLastLines("api", 1)
	if len(live) != 1 || len(stored) != 1 || live[0] != stored[0] || !utf8.ValidString(stored[0]) || !strings.HasSuffix(stored[0], truncatedLineMarker) {
		t.Fatalf("unmarked or divergent output: history=%d follow=%d", len(stored), len(live))
	}
}
