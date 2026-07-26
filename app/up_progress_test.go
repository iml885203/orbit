package app

import (
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/fatih/color"
)

func TestNextSnapshots_FirstAppearanceRecordsAllTimestampsAtNow(t *testing.T) {
	now := time.Unix(100, 0)
	got := nextSnapshots(nil, []daemon.ServiceStatus{
		{Name: "worker", State: "pending"},
	}, now)

	s, ok := got["worker"]
	if !ok {
		t.Fatalf("worker missing from snapshots")
	}
	if s.state != "pending" || !s.since.Equal(now) || !s.firstSeen.Equal(now) {
		t.Errorf("snapshot = %+v, want state=pending and both timestamps at %v", s, now)
	}
}

func TestNextSnapshots_SameStatePreservesSinceAndFirstSeen(t *testing.T) {
	prev := map[string]progressSnapshot{
		"worker": {state: "building", since: time.Unix(10, 0), firstSeen: time.Unix(0, 0), lastHeartbeat: time.Unix(30, 0)},
	}
	got := nextSnapshots(prev, []daemon.ServiceStatus{
		{Name: "worker", State: "building"},
	}, time.Unix(50, 0))

	s := got["worker"]
	if !s.since.Equal(time.Unix(10, 0)) {
		t.Errorf("since changed when state unchanged: got %v want %v", s.since, time.Unix(10, 0))
	}
	if !s.firstSeen.Equal(time.Unix(0, 0)) {
		t.Errorf("firstSeen changed: got %v want %v", s.firstSeen, time.Unix(0, 0))
	}
	if !s.lastHeartbeat.Equal(time.Unix(30, 0)) {
		t.Errorf("lastHeartbeat lost: got %v want %v", s.lastHeartbeat, time.Unix(30, 0))
	}
}

func TestNextSnapshots_StateChangeResetsSinceKeepsFirstSeen(t *testing.T) {
	// State transition: since must move to now (so heartbeat counts
	// from the new state's start), but firstSeen must stay at the
	// original appearance time (so healthy elapsed reflects the
	// whole startup, not just the last state).
	prev := map[string]progressSnapshot{
		"worker": {state: "building", since: time.Unix(10, 0), firstSeen: time.Unix(0, 0), lastHeartbeat: time.Unix(40, 0)},
	}
	got := nextSnapshots(prev, []daemon.ServiceStatus{
		{Name: "worker", State: "starting"},
	}, time.Unix(50, 0))

	s := got["worker"]
	if !s.since.Equal(time.Unix(50, 0)) {
		t.Errorf("since not reset on state change: got %v want %v", s.since, time.Unix(50, 0))
	}
	if !s.firstSeen.Equal(time.Unix(0, 0)) {
		t.Errorf("firstSeen lost on state change: got %v want %v", s.firstSeen, time.Unix(0, 0))
	}
	if !s.lastHeartbeat.IsZero() {
		t.Errorf("lastHeartbeat should reset on state change: got %v", s.lastHeartbeat)
	}
}

func TestEffectiveTimeout_ZeroFallsBackToDefault(t *testing.T) {
	if got := effectiveTimeout(0); got != defaultWaitTimeout {
		t.Errorf("got %v, want %v", got, defaultWaitTimeout)
	}
}

func TestEffectiveTimeout_NonZeroPassesThrough(t *testing.T) {
	if got := effectiveTimeout(30 * time.Second); got != 30*time.Second {
		t.Errorf("got %v, want 30s", got)
	}
}

func TestNextSnapshots_FilterSelectsSubsetOfServices(t *testing.T) {
	// The caller of nextSnapshots is responsible for only feeding it
	// services it wants to track — we don't add a filter knob inside.
	// This documents that contract: every status entry becomes a
	// snapshot entry.
	got := nextSnapshots(nil, []daemon.ServiceStatus{
		{Name: "worker", State: "pending"},
		{Name: "payments", State: "pending"},
	}, time.Unix(0, 0))
	if len(got) != 2 {
		t.Errorf("want 2 snapshots, got %d", len(got))
	}
}

// progress events are the lines we want printed during `orbit up` so the
// user can see what the daemon is actually doing instead of guessing
// during a long silence. diffProgress turns two consecutive status
// snapshots (plus how long the current state has been observed) into
// the events to render.

func TestDiffProgress_StateTransitionEmitsEvent(t *testing.T) {
	prev := map[string]progressSnapshot{
		"worker": {state: "pending", since: time.Unix(0, 0)},
	}
	curr := map[string]progressSnapshot{
		"worker": {state: "starting", since: time.Unix(1, 0)},
	}
	got := diffProgress(prev, curr, time.Unix(2, 0))

	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d: %+v", len(got), got)
	}
	if got[0].name != "worker" || got[0].from != "pending" || got[0].to != "starting" {
		t.Errorf("transition wrong: %+v", got[0])
	}
	if got[0].kind != eventTransition {
		t.Errorf("kind = %v, want eventTransition", got[0].kind)
	}
}

func TestDiffProgress_NoChangeEmitsNothing(t *testing.T) {
	prev := map[string]progressSnapshot{
		"worker": {state: "building", since: time.Unix(0, 0)},
	}
	curr := map[string]progressSnapshot{
		"worker": {state: "building", since: time.Unix(0, 0)},
	}
	got := diffProgress(prev, curr, time.Unix(5, 0))
	if len(got) != 0 {
		t.Errorf("want 0 events for unchanged state, got %d: %+v", len(got), got)
	}
}

func TestDiffProgress_HealthyEventCarriesElapsedFromFirstSeen(t *testing.T) {
	// firstSeen records when the service first appeared (entered any
	// non-terminal state). The healthy event reports total elapsed so
	// the user sees "worker healthy (39s)" not "healthy (0s)".
	prev := map[string]progressSnapshot{
		"worker": {state: "starting", since: time.Unix(0, 0), firstSeen: time.Unix(0, 0)},
	}
	curr := map[string]progressSnapshot{
		"worker": {state: "healthy", since: time.Unix(39, 0), firstSeen: time.Unix(0, 0)},
	}
	got := diffProgress(prev, curr, time.Unix(39, 0))
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	if got[0].elapsed != 39*time.Second {
		t.Errorf("elapsed = %v, want 39s", got[0].elapsed)
	}
}

func TestDiffProgress_NewServiceAppearsAsTransitionFromNone(t *testing.T) {
	prev := map[string]progressSnapshot{}
	curr := map[string]progressSnapshot{
		"worker": {state: "pending", since: time.Unix(0, 0)},
	}
	got := diffProgress(prev, curr, time.Unix(0, 0))
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	if got[0].from != "" || got[0].to != "pending" {
		t.Errorf("first appearance should have empty 'from', got %+v", got[0])
	}
}

func TestDiffProgress_LongStillRunningEmitsHeartbeat(t *testing.T) {
	// When a service has been in the same long-running state (building
	// or pre_start) for >= 30s without change, emit a heartbeat event
	// so the user knows the daemon is alive and what it's still doing.
	prev := map[string]progressSnapshot{
		"worker": {state: "building", since: time.Unix(0, 0), firstSeen: time.Unix(0, 0)},
	}
	curr := map[string]progressSnapshot{
		"worker": {state: "building", since: time.Unix(0, 0), firstSeen: time.Unix(0, 0)},
	}
	got := diffProgress(prev, curr, time.Unix(30, 0))
	if len(got) != 1 {
		t.Fatalf("want 1 heartbeat event, got %d: %+v", len(got), got)
	}
	if got[0].kind != eventHeartbeat {
		t.Errorf("kind = %v, want eventHeartbeat", got[0].kind)
	}
	if got[0].elapsed != 30*time.Second {
		t.Errorf("elapsed = %v, want 30s", got[0].elapsed)
	}
}

func TestDiffProgress_HeartbeatOnlyFiresForLongRunningStates(t *testing.T) {
	// "starting" is short-lived (process is spawning); we don't heartbeat
	// it because either it succeeds quickly or fails. "pending" means
	// blocked on deps — the user wants to know but the dep's progress
	// is the real signal, not pending's age.
	prev := map[string]progressSnapshot{
		"worker": {state: "starting", since: time.Unix(0, 0), firstSeen: time.Unix(0, 0)},
	}
	curr := map[string]progressSnapshot{
		"worker": {state: "starting", since: time.Unix(0, 0), firstSeen: time.Unix(0, 0)},
	}
	got := diffProgress(prev, curr, time.Unix(60, 0))
	if len(got) != 0 {
		t.Errorf("starting should not heartbeat, got %d events: %+v", len(got), got)
	}
}

func TestFormatProgressEvent_HealthyShowsElapsed(t *testing.T) {
	line := formatProgressEvent(progressEvent{
		kind: eventTransition, name: "worker", from: "starting", to: "healthy",
		elapsed: 39 * time.Second,
	})
	want := "  ● worker healthy (39s)"
	if line != want {
		t.Errorf("\n got: %q\nwant: %q", line, want)
	}
}

func TestFormatProgressEvent_DegradedShowsElapsed(t *testing.T) {
	line := formatProgressEvent(progressEvent{
		kind: eventTransition, name: "payments", from: "starting", to: "degraded",
		elapsed: 12 * time.Second,
	})
	want := "  ◑ payments degraded (12s)"
	if line != want {
		t.Errorf("\n got: %q\nwant: %q", line, want)
	}
}

func TestFormatProgressEvent_IntermediateTransitionShowsArrow(t *testing.T) {
	line := formatProgressEvent(progressEvent{
		kind: eventTransition, name: "worker", from: "pending", to: "building",
	})
	want := "  ⋯ worker pending → building"
	if line != want {
		t.Errorf("\n got: %q\nwant: %q", line, want)
	}
}

func TestFormatProgressEvent_FirstAppearanceOmitsArrow(t *testing.T) {
	// No "from" state means this is the service's first appearance, so
	// the arrow would be misleading ("→ pending" suggests an origin
	// state that never existed).
	line := formatProgressEvent(progressEvent{
		kind: eventTransition, name: "worker", from: "", to: "pending",
	})
	want := "  ⋯ worker pending"
	if line != want {
		t.Errorf("\n got: %q\nwant: %q", line, want)
	}
}

func TestFormatProgressEvent_HeartbeatShowsStillElapsed(t *testing.T) {
	line := formatProgressEvent(progressEvent{
		kind: eventHeartbeat, name: "worker", to: "building",
		elapsed: 30 * time.Second,
	})
	want := "  ⋯ worker still building (30s)"
	if line != want {
		t.Errorf("\n got: %q\nwant: %q", line, want)
	}
}

func TestDiffProgress_HeartbeatOnlyOncePerInterval(t *testing.T) {
	// Once we've emitted a heartbeat at 30s, the next one shouldn't fire
	// at 31s — it should wait another full interval. lastHeartbeat
	// tracks the timestamp of the most recent heartbeat we printed.
	prev := map[string]progressSnapshot{
		"worker": {
			state:         "building",
			since:         time.Unix(0, 0),
			firstSeen:     time.Unix(0, 0),
			lastHeartbeat: time.Unix(30, 0),
		},
	}
	curr := map[string]progressSnapshot{
		"worker": {
			state:         "building",
			since:         time.Unix(0, 0),
			firstSeen:     time.Unix(0, 0),
			lastHeartbeat: time.Unix(30, 0),
		},
	}
	got := diffProgress(prev, curr, time.Unix(45, 0))
	if len(got) != 0 {
		t.Errorf("heartbeat should not refire before interval, got %d events", len(got))
	}
}

func TestFormatProgressEvent_StoppedShowsHollowCircleAndElapsed(t *testing.T) {
	line := formatProgressEvent(progressEvent{
		kind: eventTransition, name: "payments", from: "stopping", to: "stopped",
		elapsed: 3 * time.Second,
	})
	want := "  ○ payments stopped (3s)"
	if line != want {
		t.Errorf("\n got: %q\nwant: %q", line, want)
	}
}

func TestColoredEvent_StoppedAppliesFaintNotGreenOrRed(t *testing.T) {
	prev := color.NoColor
	prevGreen := cli.Green
	prevFaint := cli.Faint
	color.NoColor = false
	cli.Green = color.New(color.FgGreen)
	cli.Faint = color.New(color.Faint)
	cli.Green.EnableColor()
	cli.Faint.EnableColor()
	t.Cleanup(func() {
		color.NoColor = prev
		cli.Green = prevGreen
		cli.Faint = prevFaint
	})

	stopped := progressEvent{kind: eventTransition, name: "x", to: "stopped"}
	healthy := progressEvent{kind: eventTransition, name: "x", to: "healthy"}
	plain := "test-line"

	stoppedOut := coloredEvent(stopped, plain)
	healthyOut := coloredEvent(healthy, plain)

	if !strings.Contains(stoppedOut, plain) {
		t.Errorf("stopped should preserve line text, got: %q", stoppedOut)
	}
	if stoppedOut == healthyOut {
		t.Errorf("stopped and healthy should use different colours, both got %q", stoppedOut)
	}
}
