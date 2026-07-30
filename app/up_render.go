package app

import (
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/iml885203/orbit/daemon"
	"golang.org/x/term"
)

// spinnerChars cycles through the standard braille spinner used by
// npm/pnpm/yarn. Frame index is taken mod len(spinnerChars).
var spinnerChars = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

// minNameWidth keeps the columns from jumping when only short names are
// present. 12 fits "sql-server" with a little headroom.
const minNameWidth = 12

// maxLineWidth is a hard fallback when terminal width cannot be detected
// (non-TTY, errors from term.GetSize). The live renderer counts logical
// lines and clears N rows on redraw — wrapped rows leak into scrollback
// and break the in-place region, so every line gets truncated to fit.
const maxLineWidth = 100

// terminalWidth reports the current stdout terminal width, or
// maxLineWidth if it cannot be detected.
func terminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return maxLineWidth
	}
	return w
}

// renderLines returns one line per non-terminal service in snapshots,
// formatted for the live in-place region. Healthy/degraded services are
// expected to have been committed elsewhere; renderLines never emits
// them. progressByName is allowed to be nil — services with no entry
// (or with nil HealthProgress) render without the health columns.
func renderLines(
	snapshots map[string]progressSnapshot,
	progressByName map[string]*daemon.HealthProgressInfo,
	now time.Time,
	frame int,
) []string {
	names := inPlaceNames(snapshots)
	if len(names) == 0 {
		return nil
	}
	nameWidth := computeNameWidth(names)
	glyph := string(spinnerChars[frame%len(spinnerChars)])

	out := make([]string, 0, len(names))
	for _, name := range names {
		s := snapshots[name]
		line := fmt.Sprintf("  %s %-*s %s %s",
			glyph,
			nameWidth, name,
			s.state,
			fmtDur(now.Sub(s.firstSeen)),
		)
		if p := progressByName[name]; p != nil {
			line += fmt.Sprintf("  health %d/%d", p.Attempts, p.MaxRetries)
			if p.LastErr != "" {
				line += "  last: " + p.LastErr
			}
		}
		out = append(out, truncateLine(line))
	}
	return out
}

// truncateLine caps a rendered line at the current terminal width minus
// 1 char of safety margin (some terminals wrap when the cursor hits the
// last column, not when it passes it). When truncation happens, the last
// 3 chars become "..." so the user knows content was elided.
func truncateLine(line string) string {
	w := terminalWidth() - 1
	if w < 20 {
		w = 20 // pathologically narrow terminals — still leave something readable
	}
	if len(line) <= w {
		return line
	}
	return line[:w-3] + "..."
}

// inPlaceNames returns the sorted set of service names that still belong
// in the live region (anything not terminal).
func inPlaceNames(snapshots map[string]progressSnapshot) []string {
	names := make([]string, 0, len(snapshots))
	for name, s := range snapshots {
		if isTerminalState(s.state) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func isTerminalState(state string) bool {
	return state == "healthy" || state == "degraded" || state == "stopped"
}

func computeNameWidth(names []string) int {
	w := minNameWidth
	for _, n := range names {
		if len(n) > w {
			w = len(n)
		}
	}
	return w
}

// progressRenderer abstracts how progress is displayed during orbit up.
// Two implementations exist: liveRenderer (TTY, in-place redraw with
// spinner) and appendRenderer (non-TTY fallback, one line per event).
type progressRenderer interface {
	// render is called every frame on TTY; appendRenderer ignores it.
	render(snapshots map[string]progressSnapshot, progressByName map[string]*daemon.HealthProgressInfo, now time.Time, frame int)
	// commit promotes a transition into permanent output (above any
	// in-place region).
	commit(line string)
	// finalize wraps the run. success=true clears live region; success=false
	// freezes it for scrollback debug.
	finalize(success bool)
}

// appendRenderer writes committed lines verbatim and ignores render. It
// is selected when stdout is not a TTY: CI logs, files via redirect, etc.
type appendRenderer struct {
	out io.Writer
}

func newAppendRenderer(out io.Writer) *appendRenderer {
	return &appendRenderer{out: out}
}

// compile-time check that *appendRenderer implements progressRenderer.
var _ progressRenderer = (*appendRenderer)(nil)

func (r *appendRenderer) render(_ map[string]progressSnapshot, _ map[string]*daemon.HealthProgressInfo, _ time.Time, _ int) {
	// non-TTY: no in-place region, just print commits as they come
}

func (r *appendRenderer) commit(line string) {
	_, _ = fmt.Fprintln(r.out, line)
}

func (r *appendRenderer) finalize(success bool) {
	// nothing — commit lines are already in the output stream
	_ = success
}

// ANSI sequences used by liveRenderer. Bundled here so a future colour
// strategy can swap them without touching the renderer logic.
const (
	ansiCursorUp     = "\x1b[%dA" // %d lines up
	ansiClearLine    = "\x1b[2K"  // erase entire line
	ansiCursorToCol1 = "\r"
)

// liveRenderer maintains an in-place region of progress lines that gets
// cleared and redrawn each frame. Committed lines (services that have
// finished) are flushed permanently above the region and never redrawn.
type liveRenderer struct {
	out           io.Writer
	lastLineCount int
}

// Compile-time interface check.
var _ progressRenderer = (*liveRenderer)(nil)

func newLiveRenderer(out io.Writer) *liveRenderer {
	return &liveRenderer{out: out}
}

func (r *liveRenderer) render(
	snapshots map[string]progressSnapshot,
	progressByName map[string]*daemon.HealthProgressInfo,
	now time.Time,
	frame int,
) {
	r.clearRegion()
	lines := renderLines(snapshots, progressByName, now, frame)
	for _, line := range lines {
		_, _ = fmt.Fprintln(r.out, line)
	}
	r.lastLineCount = len(lines)
}

func (r *liveRenderer) commit(line string) {
	// Wipe the in-place region first so the commit lands above it.
	r.clearRegion()
	r.lastLineCount = 0
	_, _ = fmt.Fprintln(r.out, line)
}

func (r *liveRenderer) finalize(success bool) {
	if success {
		// Clear the in-place region — the final summary line replaces it.
		r.clearRegion()
		r.lastLineCount = 0
		return
	}
	// Failure: freeze the region as scrollback. No clearing.
}

// clearRegion moves the cursor up over the last in-place region, erases
// each row, and leaves the cursor at column 1 of the topmost cleared
// row — ready for the next caller to write the replacement content.
// No-op when nothing was drawn.
func (r *liveRenderer) clearRegion() {
	if r.lastLineCount == 0 {
		return
	}
	_, _ = fmt.Fprint(r.out, ansiCursorToCol1)
	_, _ = fmt.Fprintf(r.out, ansiCursorUp, r.lastLineCount)
	// Clear each row in place using a sequence that erases the line
	// without moving the cursor down. After this loop, cursor is back
	// at the top-left of the region.
	for i := 0; i < r.lastLineCount; i++ {
		_, _ = fmt.Fprint(r.out, ansiClearLine)
		if i < r.lastLineCount-1 {
			_, _ = fmt.Fprint(r.out, "\n")
		}
	}
	if r.lastLineCount > 1 {
		// Move cursor back up to the top of the region.
		_, _ = fmt.Fprintf(r.out, ansiCursorUp, r.lastLineCount-1)
	}
	_, _ = fmt.Fprint(r.out, ansiCursorToCol1)
}
