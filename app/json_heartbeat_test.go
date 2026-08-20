package app

import (
	"testing"
	"time"

	"github.com/iml885203/orbit/daemon"
)

// `up --json` returned nothing between the call and its result — for one
// reported CI run, 500 seconds of silence with a resource sitting in
// "building". The heartbeat machinery already existed on the human path;
// runUpJSON forked before reaching it. These pin the accounting, because a
// heartbeat that repeats every poll is worse than none.
func TestJSONHeartbeatAccounting(t *testing.T) {
	now := time.Now()
	building := []daemon.ResourceStatus{{Name: "api", State: "building"}}

	// First sight of a resource is a transition, not a heartbeat: nothing has
	// been silent yet.
	snaps := emitJSONHeartbeats(map[string]progressSnapshot{},
		nextSnapshots(map[string]progressSnapshot{}, building, now), now)
	if !snaps["api"].lastHeartbeat.IsZero() {
		t.Error("heartbeat recorded on first sight, before any silence")
	}

	// A full interval of the same state is what a heartbeat reports.
	due := now.Add(heartbeatInterval + time.Second)
	snaps = emitJSONHeartbeats(snaps, nextSnapshots(snaps, building, due), due)
	if snaps["api"].lastHeartbeat.IsZero() {
		t.Fatal("no heartbeat after a full interval; the silence this fixes would remain")
	}

	// Recording it is what stops the next poll, 500ms later, from repeating it.
	soon := due.Add(500 * time.Millisecond)
	snaps = emitJSONHeartbeats(snaps, nextSnapshots(snaps, building, soon), soon)
	if got := snaps["api"].lastHeartbeat; !got.Equal(due) {
		t.Errorf("lastHeartbeat = %v, want it held at %v so the beat does not repeat per poll", got, due)
	}
}

// A service waiting on its health check reports "starting" for as long as its
// retries allow. Treating that window as too short to report left a local run
// silent for 75s with the service alive and failing its probe — the shape a
// caller reads as a hang.
func TestJSONHeartbeatCoversAServiceStuckStarting(t *testing.T) {
	now := time.Now()
	starting := []daemon.ResourceStatus{{Name: "api", State: "starting"}}

	snaps := nextSnapshots(map[string]progressSnapshot{}, starting, now)
	due := now.Add(heartbeatInterval + time.Second)
	snaps = emitJSONHeartbeats(snaps, nextSnapshots(snaps, starting, due), due)

	if snaps["api"].lastHeartbeat.IsZero() {
		t.Error("no heartbeat for a service still starting after a full interval")
	}
}

// The point of covering "starting" is the stuck case, not the ordinary one. A
// service that comes up promptly must not gain a line, or the heartbeat stops
// meaning anything.
func TestJSONHeartbeatSilentForAPromptStart(t *testing.T) {
	now := time.Now()
	starting := []daemon.ResourceStatus{{Name: "api", State: "starting"}}
	snaps := nextSnapshots(map[string]progressSnapshot{}, starting, now)

	// Healthy well inside one interval, as a normal start is.
	soon := now.Add(heartbeatInterval / 3)
	healthy := []daemon.ResourceStatus{{Name: "api", State: "healthy"}}
	snaps = emitJSONHeartbeats(snaps, nextSnapshots(snaps, healthy, soon), soon)

	if !snaps["api"].lastHeartbeat.IsZero() {
		t.Error("a prompt start produced a heartbeat")
	}
}

// Only long-running states earn a heartbeat. A resource that has settled —
// healthy, degraded — or one whose real signal is its dependency's progress
// must not produce output; the value of the line is that it means "still
// working".
func TestJSONHeartbeatOnlyForLongRunningStates(t *testing.T) {
	now := time.Now()
	for _, state := range []string{"healthy", "pending", "degraded", "stopped"} {
		t.Run(state, func(t *testing.T) {
			rs := []daemon.ResourceStatus{{Name: "api", State: state}}
			snaps := nextSnapshots(map[string]progressSnapshot{}, rs, now)
			later := now.Add(5 * time.Minute)
			snaps = emitJSONHeartbeats(snaps, nextSnapshots(snaps, rs, later), later)
			if !snaps["api"].lastHeartbeat.IsZero() {
				t.Errorf("state %q produced a heartbeat", state)
			}
		})
	}
}
