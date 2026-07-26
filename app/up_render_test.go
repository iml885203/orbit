package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/daemon"
)

func TestAppendRenderer_PrintsCommittedLinesOnceEach(t *testing.T) {
	var buf bytes.Buffer
	r := newAppendRenderer(&buf)
	r.commit("  ● a healthy (5s)")
	r.commit("  ● b healthy (7s)")
	r.render(nil, nil, time.Unix(0, 0), 0) // append renderer ignores render calls
	r.finalize(true)

	got := buf.String()
	if strings.Count(got, "healthy") != 2 {
		t.Errorf("expected 2 healthy lines, got: %q", got)
	}
}

func TestAppendRenderer_RenderIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	r := newAppendRenderer(&buf)
	r.render(map[string]progressSnapshot{
		"a": {state: "starting"},
	}, nil, time.Unix(0, 0), 0)
	if buf.Len() != 0 {
		t.Errorf("append renderer should not emit on render(), got %q", buf.String())
	}
}

func TestRenderLines_HealthyServicesNotIncludedInInPlaceRegion(t *testing.T) {
	now := time.Unix(10, 0)
	snapshots := map[string]progressSnapshot{
		"a": {state: "healthy", firstSeen: time.Unix(0, 0), since: time.Unix(5, 0)},
		"b": {state: "starting", firstSeen: time.Unix(0, 0), since: time.Unix(0, 0)},
	}
	lines := renderLines(snapshots, nil, now, 0)
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "b") {
		t.Errorf("line should be for service b, got %q", lines[0])
	}
}

func TestRenderLines_PadsNameWidthAcrossServices(t *testing.T) {
	now := time.Unix(0, 0)
	snapshots := map[string]progressSnapshot{
		"a":          {state: "starting"},
		"sql-server": {state: "starting"},
	}
	lines := renderLines(snapshots, nil, now, 0)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	// Both lines should align: find the index of "starting" in each line.
	idx1 := strings.Index(lines[0], "starting")
	idx2 := strings.Index(lines[1], "starting")
	if idx1 != idx2 {
		t.Errorf("name column not padded consistently: %q vs %q", lines[0], lines[1])
	}
}

func TestRenderLines_OmitsHealthColumnsWhenNoProgress(t *testing.T) {
	now := time.Unix(0, 0)
	snapshots := map[string]progressSnapshot{
		"a": {state: "starting"},
	}
	lines := renderLines(snapshots, nil, now, 0)
	if strings.Contains(lines[0], "health") {
		t.Errorf("line should omit health column when no progress, got %q", lines[0])
	}
}

func TestRenderLines_ShowsAttemptsAndLastErrWhenProgressPresent(t *testing.T) {
	now := time.Unix(0, 0)
	snapshots := map[string]progressSnapshot{
		"a": {state: "starting"},
	}
	progress := map[string]*daemon.HealthProgressInfo{
		"a": {Attempts: 9, MaxRetries: 60, LastErr: "connection refused"},
	}
	lines := renderLines(snapshots, progress, now, 0)
	if !strings.Contains(lines[0], "9/60") {
		t.Errorf("line should show 9/60, got %q", lines[0])
	}
	if !strings.Contains(lines[0], "connection refused") {
		t.Errorf("line should show last err, got %q", lines[0])
	}
}

func TestRenderLines_OmitsLastWhenErrEmpty(t *testing.T) {
	now := time.Unix(0, 0)
	snapshots := map[string]progressSnapshot{
		"a": {state: "starting"},
	}
	progress := map[string]*daemon.HealthProgressInfo{
		"a": {Attempts: 1, MaxRetries: 60, LastErr: ""},
	}
	lines := renderLines(snapshots, progress, now, 0)
	if strings.Contains(lines[0], "last:") {
		t.Errorf("line should omit last column when err empty, got %q", lines[0])
	}
}

func TestRenderLines_SpinnerGlyphDiffersAcrossFrames(t *testing.T) {
	now := time.Unix(0, 0)
	snapshots := map[string]progressSnapshot{
		"a": {state: "starting"},
	}
	a := renderLines(snapshots, nil, now, 0)[0]
	b := renderLines(snapshots, nil, now, 1)[0]
	if a == b {
		t.Errorf("expected frames 0 and 1 to render different glyphs, both got %q", a)
	}
}

func TestRenderLines_ElapsedReflectsFirstSeenNotSince(t *testing.T) {
	// firstSeen records when the service first appeared; since records
	// when current state started. Elapsed in the line is total startup
	// time, not time in current state.
	now := time.Unix(45, 0)
	snapshots := map[string]progressSnapshot{
		"a": {state: "starting", firstSeen: time.Unix(0, 0), since: time.Unix(30, 0)},
	}
	lines := renderLines(snapshots, nil, now, 0)
	if !strings.Contains(lines[0], "45s") {
		t.Errorf("elapsed should be 45s (now - firstSeen), got %q", lines[0])
	}
}

func TestRenderLines_TruncatesLongLineToAvoidTerminalWrap(t *testing.T) {
	// When a service's LastErr is very long (e.g. a full HTTP error like
	// `Get "http://localhost:5056/health": connection refused`), the
	// rendered line can exceed the terminal width and the terminal wraps
	// it onto a second visual row. liveRenderer counts logical lines and
	// only clears N rows on redraw, so wrapped rows leak into scrollback
	// and the spinner appears to "scroll" instead of staying in place.
	now := time.Unix(0, 0)
	snapshots := map[string]progressSnapshot{
		"a": {state: "starting"},
	}
	progress := map[string]*daemon.HealthProgressInfo{
		"a": {Attempts: 4, MaxRetries: 60, LastErr: strings.Repeat("x", 500)},
	}
	lines := renderLines(snapshots, progress, now, 0)
	if len(lines[0]) > maxLineWidth {
		t.Errorf("line should be truncated to %d chars, got %d: %q", maxLineWidth, len(lines[0]), lines[0])
	}
}

func TestLiveRenderer_RenderEmitsCurrentInPlaceLines(t *testing.T) {
	var buf bytes.Buffer
	r := newLiveRenderer(&buf)
	r.render(map[string]progressSnapshot{
		"payments": {state: "starting", firstSeen: time.Unix(0, 0)},
	}, nil, time.Unix(5, 0), 0)

	got := buf.String()
	if !strings.Contains(got, "payments") || !strings.Contains(got, "starting") {
		t.Errorf("render output missing service line: %q", got)
	}
	if !strings.Contains(got, "5s") {
		t.Errorf("render output missing elapsed: %q", got)
	}
}

func TestLiveRenderer_SecondRenderClearsFirst(t *testing.T) {
	// Two renders in a row should emit ANSI cursor control to clear
	// the prior region before drawing again. Easier check: the second
	// render's output contains an ANSI escape sequence.
	var buf bytes.Buffer
	r := newLiveRenderer(&buf)
	r.render(map[string]progressSnapshot{
		"a": {state: "starting", firstSeen: time.Unix(0, 0)},
	}, nil, time.Unix(1, 0), 0)
	posBefore := buf.Len()
	r.render(map[string]progressSnapshot{
		"a": {state: "starting", firstSeen: time.Unix(0, 0)},
		"b": {state: "starting", firstSeen: time.Unix(0, 0)},
	}, nil, time.Unix(2, 0), 0)
	delta := buf.String()[posBefore:]
	if !strings.Contains(delta, "\x1b[") {
		t.Errorf("second render should emit ANSI cursor control before redraw, got %q", delta)
	}
}

func TestLiveRenderer_FinalizeFailureKeepsInPlaceRegion(t *testing.T) {
	// On failure (B2 spec), the live region freezes; no clearing
	// sequence is emitted as part of finalize.
	var buf bytes.Buffer
	r := newLiveRenderer(&buf)
	r.render(map[string]progressSnapshot{
		"a": {state: "starting", firstSeen: time.Unix(0, 0)},
	}, nil, time.Unix(0, 0), 0)
	posBefore := buf.Len()
	r.finalize(false)
	delta := buf.String()[posBefore:]
	if strings.Contains(delta, "\x1b[2K") {
		t.Errorf("failure finalize should not clear region, delta was %q", delta)
	}
}

func TestLiveRenderer_CommittedLinesSurviveRedraw(t *testing.T) {
	var buf bytes.Buffer
	r := newLiveRenderer(&buf)
	r.commit("  ● a healthy (3s)")
	r.render(map[string]progressSnapshot{
		"b": {state: "starting", firstSeen: time.Unix(0, 0)},
	}, nil, time.Unix(5, 0), 0)
	// Commit line is written immediately, before the in-place region.
	out := buf.String()
	commitIdx := strings.Index(out, "● a healthy")
	bIdx := strings.Index(out, "b")
	if commitIdx == -1 || bIdx == -1 || commitIdx > bIdx {
		t.Errorf("commit line should appear before in-place region; got %q", out)
	}
}

func TestRenderLines_StoppedServicesNotIncludedInInPlaceRegion(t *testing.T) {
	now := time.Unix(10, 0)
	snapshots := map[string]progressSnapshot{
		"a": {state: "stopped", firstSeen: time.Unix(0, 0), since: time.Unix(5, 0)},
		"b": {state: "stopping", firstSeen: time.Unix(0, 0), since: time.Unix(0, 0)},
	}
	lines := renderLines(snapshots, nil, now, 0)
	if len(lines) != 1 {
		t.Fatalf("want 1 line (b only), got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "b") {
		t.Errorf("line should be for service b, got %q", lines[0])
	}
}
