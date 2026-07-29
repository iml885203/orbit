package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/iml885203/orbit/port"
)

// ServiceState represents the lifecycle state of a service or container.
type ServiceState int

const (
	StatePending ServiceState = iota
	StateBuilding
	StateStarting
	StateHealthy
	StateDegraded
	StateStopping
	StateStopped
	StateRestarting
)

func (s ServiceState) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateBuilding:
		return "building"
	case StateStarting:
		return "starting"
	case StateHealthy:
		return "healthy"
	case StateDegraded:
		return "degraded"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	case StateRestarting:
		return "restarting"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// EventType represents orchestrator events.
type EventType int

const (
	EventDepsReady EventType = iota
	EventHealthOK
	EventHealthFail
	EventProcessExited
	EventContainerDrift
	EventBuildStarted
	EventBuildComplete
	EventBuildFailed
	EventShutdown
	EventServiceLog
)

func (e EventType) String() string {
	switch e {
	case EventDepsReady:
		return "DepsReady"
	case EventHealthOK:
		return "HealthOK"
	case EventHealthFail:
		return "HealthFail"
	case EventProcessExited:
		return "ProcessExited"
	case EventContainerDrift:
		return "ContainerDrift"
	case EventBuildStarted:
		return "BuildStarted"
	case EventBuildComplete:
		return "BuildComplete"
	case EventBuildFailed:
		return "BuildFailed"
	case EventShutdown:
		return "Shutdown"
	case EventServiceLog:
		return "ServiceLog"
	default:
		return fmt.Sprintf("unknown(%d)", int(e))
	}
}

// Event is sent through the orchestrator event channel.
type Event struct {
	Type     EventType
	Service  string
	Message  string
	Evidence string
	Err      error
	// Generation identifies which start of the service produced a health
	// event. handleEvent drops health events whose generation doesn't match
	// the service's current one: a probe goroutine from a previous start —
	// cancelled, but with a success already in flight — must not flip the
	// new generation's state. Zero means unversioned (non-health events,
	// which carry their own state guards).
	Generation int
}

// ExternalContainerRestart is emitted when Docker reports a new runtime
// without a corresponding Orbit start action.
type ExternalContainerRestart struct {
	Name       string
	StartedAt  time.Time
	ObservedAt time.Time
}

// ServiceInfo holds runtime state for a single service/container.
type ServiceInfo struct {
	Name         string
	Kind         string // "container" or "service"
	State        ServiceState
	PendingDeps  map[string]bool // deps not yet healthy
	RestartCount int
	// ExternalRestartCount counts container starts observed without a
	// matching Orbit lifecycle action. ContainerStartedAt is Docker's
	// authoritative timestamp for the current runtime; it keeps uptime
	// honest when a user or another tool restarts the container.
	ExternalRestartCount  int
	ContainerStartedAt    time.Time
	LastExternalRestart   time.Time
	LastExternalStartedAt time.Time
	// A Docker API can answer before all runtimes are visible while the daemon
	// itself is recovering. The count prevents that partial snapshot from
	// being mistaken for a user removing every container.
	ContainerMissingObservations int
	// ExpectingContainerStart prevents an Orbit-managed start from being
	// misclassified when Docker reports the replacement runtime.
	ExpectingContainerStart bool
	// Generation counts how many times the service has entered Starting.
	// Health events are stamped with it so a stale probe goroutine from a
	// previous start can't affect the current one. Mutation requires the
	// owning Orchestrator's mu.
	Generation int
	// StateReason says why the service is Degraded (crash message, health
	// failure, build failure). Populated only while State is Degraded —
	// Transition clears it on every other target so a stale reason never
	// survives a restart. Mutation requires the owning Orchestrator's mu.
	StateReason string
	// FailureEvidence preserves the last meaningful stderr line captured from
	// the process generation associated with the current degradation. It is
	// cleared with StateReason so evidence from an earlier generation never
	// survives recovery.
	// Mutation requires the owning Orchestrator's mu.
	FailureEvidence string
	PortConflict    *port.ConflictError
	// AwaitingContainerRemoval distinguishes a failed stop whose Docker
	// removal may still finish from a startup failure where no container
	// ever existed. The poller may reconcile only the former to stopped.
	AwaitingContainerRemoval bool
	StartedAt                time.Time // when the service entered Starting state
	HealthyAt                time.Time // when the service first became Healthy

	// ctx is the per-service lifecycle context. It is created when the
	// service transitions into Starting and cancelled by StopService/
	// RestartService. Health checks, wait strategies, and child process
	// spawns derive from it so a stop mid-startup aborts them instead of
	// leaking zombies.
	ctx    context.Context
	cancel context.CancelFunc
}

// cancelLifecycle cancels the per-service lifecycle context, if any, and
// clears the cancel func so subsequent calls are no-ops. Safe on nil
// receiver and on already-cancelled contexts. Caller must hold the
// owning Orchestrator's mutex — ServiceInfo does not have its own lock.
func (i *ServiceInfo) cancelLifecycle() {
	if i == nil || i.cancel == nil {
		return
	}
	i.cancel()
	i.cancel = nil
}

// MarkRestart bumps the restart counter. Called by RestartService;
// kept separate from Transition because restarting is a property of
// the action, not the state being entered. Caller must hold the
// owning Orchestrator's mutex.
func (i *ServiceInfo) MarkRestart() {
	i.RestartCount++
}

// MarkDependencyReady removes dep from the service's PendingDeps and
// reports whether the service is now waiting on nothing (so the caller
// can fire EventDepsReady). Returns false if the dep was not in
// PendingDeps — that lets callers tell "this service wasn't waiting on
// us" apart from "this service is now ready". Caller must hold the
// owning Orchestrator's mutex.
func (i *ServiceInfo) MarkDependencyReady(dep string) (becameReady bool) {
	if _, waiting := i.PendingDeps[dep]; !waiting {
		return false
	}
	delete(i.PendingDeps, dep)
	return len(i.PendingDeps) == 0
}

// Transition moves the service into a new state and applies any
// side-effects tied to that state (timestamp updates, lifecycle ctx
// cancellation). Centralising the side-effects here keeps every caller
// honest: setting State directly will skip them. Caller must hold the
// owning Orchestrator's mutex.
//
// Transitions are advisory, not validated — this method does not refuse
// any state change. Validation belongs in the orchestrator's higher
// level decisions, not in the data type.
func (i *ServiceInfo) Transition(to ServiceState) {
	i.State = to
	if to != StateDegraded {
		// Reasons describe a degradation; entering any other state
		// invalidates them. Callers set StateReason right after a
		// Transition(StateDegraded).
		i.StateReason = ""
		i.FailureEvidence = ""
		i.PortConflict = nil
		i.AwaitingContainerRemoval = false
	}
	switch to {
	case StateStarting:
		i.StartedAt = time.Now()
		i.HealthyAt = time.Time{}
	case StateHealthy:
		if i.HealthyAt.IsZero() {
			i.HealthyAt = time.Now()
		}
	case StateStopping, StateStopped:
		i.cancelLifecycle()
	}
	// StatePending / StateBuilding / StateDegraded intentionally have no
	// side effects: callers reassign PendingDeps explicitly via
	// calcPendingDeps when entering Pending, and Building/Degraded are
	// labels with no associated timestamps in the current design.
}
