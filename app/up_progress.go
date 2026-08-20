package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

// progressSnapshot is what we remember about each service between status
// polls so we can detect transitions and emit timely progress events.
type progressSnapshot struct {
	state               string
	reason              string
	portConflict        *daemon.ResourcePortConflict
	recovering          bool
	pendingDependencies []string
	since               time.Time // when we first observed this state
	firstSeen           time.Time // when the service first appeared in any non-terminal state
	lastHeartbeat       time.Time // when we last emitted a heartbeat for this service in this state
}

type progressEventKind int

const (
	eventTransition progressEventKind = iota
	eventHeartbeat
)

type progressEvent struct {
	kind    progressEventKind
	name    string
	from    string
	to      string
	elapsed time.Duration
}

// heartbeatInterval is how long a service can sit in a long-running state
// (building / pre_start) before we tell the user we're still alive.
const heartbeatInterval = 30 * time.Second

// defaultWaitTimeout is the upper bound on how long `orbit up` waits for
// services to settle before giving up. Callers may override via the
// `--timeout` flag (handled at the cmd layer, not here).
const defaultWaitTimeout = 5 * time.Minute

// effectiveTimeout falls back to defaultWaitTimeout when the user did not
// supply --timeout (raw flag value is zero), so the three waitFor* loops
// share one bound instead of each hardcoding "5 * time.Minute".
func effectiveTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultWaitTimeout
	}
	return d
}

// newWaitTimeoutError reports which budget ran out, not just that one did.
//
// Orbit enforces two independent budgets and neither knows about the other:
// this one bounds how long the CLI waits, while a service's own
// health_check.retries bounds how many times the daemon probes it. Whichever
// expires first ends the wait. A user who raises retries and still times out
// here has no way to tell from "timeout waiting for resources to become
// healthy" that they raised a budget that was never the one being spent.
//
// waited is reported alongside the budget because the two do not match: the
// clock starts when the wait loop does, so daemon startup and environment
// convergence land outside it. Printing only the budget invites the reader to
// check it against a wall-clock duration that was always going to be larger.
// requested is the caller's raw --timeout value before the fallback, so
// attribution survives `--timeout 5m`: that budget is indistinguishable from
// the default by value, and telling someone who set it to "raise it with
// --timeout" names a step they have already taken.
func newWaitTimeoutError(what string, requested, budget, waited time.Duration) error {
	source := "--timeout"
	remedy := "Raise --timeout, or check whether the service is stuck rather than slow."
	if requested <= 0 {
		source = "the default"
		remedy = "Raise it with --timeout."
	}
	// waited always exceeds budget slightly — the deadline fires a moment
	// after it expires — so the note is keyed on the rounded value the reader
	// actually sees. Explaining a discrepancy between two numbers that both
	// print as "25s" describes a contradiction that is not on screen.
	shown := waited.Round(time.Second)
	elapsedNote := ""
	if shown > budget {
		elapsedNote = " The budget covers only the wait, which starts after the daemon is up and the environment has converged."
	}
	return cli.NewTimeoutError(fmt.Sprintf(
		"timeout waiting for %s — waited %s against a %s budget from %s.%s "+
			"This is the CLI's wait budget, separate from each service's "+
			"health_check.retries; whichever expires first ends the wait. %s",
		what, shown, budget, source, elapsedNote, remedy,
	))
}

// heartbeatable lists states where silence likely means "still working,"
// not "blocked." For "pending" the dep's progress is the better signal.
//
// "starting" is here despite covering a window that is usually brief: it runs
// from spawn until the health check passes, so it also covers every retry of a
// check that is not passing yet. A service stuck there is silent for as long
// as its retries allow — measured at 75s in one local run, and bounded only by
// health_check.retries. Reaching the interval at all means the window was not
// brief, which is exactly when the reader needs to hear something; a service
// that starts normally becomes healthy well inside one interval and stays
// quiet.
var heartbeatable = map[string]bool{
	"building":  true,
	"pre_start": true,
	"starting":  true,
	"stopping":  true,
}

// nextSnapshots builds the snapshot map for this poll from the previous
// snapshots and the current status entries. firstSeen is preserved across
// polls so healthy events can report total startup elapsed; since is reset
// only when state actually changes, so heartbeat intervals count from the
// current state's start; lastHeartbeat clears on state change so a fresh
// state gets its first heartbeat after a full interval.
func nextSnapshots(prev map[string]progressSnapshot, statuses []daemon.ResourceStatus, now time.Time) map[string]progressSnapshot {
	out := make(map[string]progressSnapshot, len(statuses))
	for i := range statuses {
		svc := &statuses[i]
		snap := progressSnapshot{
			state:               svc.State,
			reason:              svc.StateReason,
			portConflict:        svc.PortConflict,
			pendingDependencies: append([]string{}, svc.PendingDependencies...),
			since:               now,
			firstSeen:           now,
		}
		if svc.HealthProgress != nil {
			snap.recovering = svc.HealthProgress.Recovering
			if snap.reason == "" {
				snap.reason = svc.HealthProgress.LastErr
			}
		}
		if p, ok := prev[svc.Name]; ok {
			snap.firstSeen = p.firstSeen
			if p.state == svc.State {
				snap.since = p.since
				snap.lastHeartbeat = p.lastHeartbeat
			}
		}
		out[svc.Name] = snap
	}
	return out
}

// diffProgress computes the events to print at time now given the prev
// snapshot the caller last saw and the curr snapshot from the most
// recent status poll. Pure function so we can test it without mocking
// the daemon client or the clock.
func diffProgress(prev, curr map[string]progressSnapshot, now time.Time) []progressEvent {
	var events []progressEvent
	for name, c := range curr {
		p, existed := prev[name]
		if !existed || p.state != c.state {
			events = append(events, progressEvent{
				kind:    eventTransition,
				name:    name,
				from:    p.state,
				to:      c.state,
				elapsed: now.Sub(c.firstSeen),
			})
			continue
		}
		if heartbeatable[c.state] && now.Sub(maxTime(c.since, c.lastHeartbeat)) >= heartbeatInterval {
			events = append(events, progressEvent{
				kind:    eventHeartbeat,
				name:    name,
				to:      c.state,
				elapsed: now.Sub(c.since),
			})
		}
	}
	return events
}

// emitJSONHeartbeats writes one line per heartbeat to stderr and returns the
// snapshots with those heartbeats recorded, so the next poll waits a full
// interval before repeating itself.
//
// stdout carries the `orbit.cli.v1` envelope and nothing else, which is why
// progress goes to stderr: a consumer parsing stdout is unaffected, while one
// watching the run — a CI log, a person — stops facing an unbroken silence
// between the call and its result. The wait can run for minutes, and without
// this there is no way to tell a slow build from a stuck one.
//
// Transitions are deliberately not emitted. The envelope already reports the
// final state of every resource; what it cannot report is that something was
// still moving while the caller waited.
func emitJSONHeartbeats(prev, next map[string]progressSnapshot, now time.Time) map[string]progressSnapshot {
	for _, evt := range diffProgress(prev, next, now) {
		if evt.kind != eventHeartbeat {
			continue
		}
		fmt.Fprintf(os.Stderr, "%s\n", strings.TrimSpace(formatProgressEvent(evt)))
		snap := next[evt.name]
		snap.lastHeartbeat = now
		next[evt.name] = snap
	}
	return next
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// formatProgressEvent renders an event as a single text line. Colour is
// intentionally not applied here so tests can compare raw strings; the
// caller wraps the icon and state words in ANSI codes when printing
// to a terminal.
func formatProgressEvent(e progressEvent) string {
	switch e.kind {
	case eventHeartbeat:
		return fmt.Sprintf("  ⋯ %s still %s (%s)", e.name, e.to, fmtDur(e.elapsed))
	case eventTransition:
		switch e.to {
		case "healthy":
			return fmt.Sprintf("  ● %s healthy (%s)", e.name, fmtDur(e.elapsed))
		case "degraded":
			return fmt.Sprintf("  ◑ %s degraded (%s)", e.name, fmtDur(e.elapsed))
		case "stopped":
			return fmt.Sprintf("  ○ %s stopped (%s)", e.name, fmtDur(e.elapsed))
		default:
			if e.from == "" {
				return fmt.Sprintf("  ⋯ %s %s", e.name, e.to)
			}
			return fmt.Sprintf("  ⋯ %s %s → %s", e.name, e.from, e.to)
		}
	}
	return ""
}

func fmtDur(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}
