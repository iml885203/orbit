package engine

import (
	"context"
	"errors"
	"log/slog"

	"github.com/iml885203/orbit/port"
)

func (o *Orchestrator) handleEvent(ctx context.Context, evt Event) error {
	switch evt.Type {
	case EventDepsReady:
		return o.startService(ctx, evt.Service)

	case EventHealthOK:
		o.mu.Lock()
		info, exists := o.services[evt.Service]
		if !exists || staleLifecycleEvent(info, evt) {
			o.mu.Unlock()
			break
		}
		// Only the states a live probe can legitimately report on may
		// become healthy. Pending/Stopping/Stopped/Restarting must not be
		// resurrected by an in-flight success that raced a lifecycle
		// action (EventProcessExited has the mirror-image guard).
		if info.State != StateStarting && info.State != StateHealthy && info.State != StateDegraded {
			o.mu.Unlock()
			break
		}
		info.Transition(StateHealthy)
		kind := info.Kind
		o.mu.Unlock()

		if kind == "container" {
			if c, ok := o.holder.Load().Containers[evt.Service]; ok && c.Init != nil {
				if o.OnRunInit != nil {
					go func() {
						if err := o.OnRunInit(ctx, evt.Service, c); err != nil {
							slog.Error("init failed", "component", "orchestrator", "service", evt.Service, "err", err)
						}
					}()
				}
			}
		}

		// Notify dependents
		o.notifyDependents(evt.Service)

	case EventHealthFail:
		o.mu.Lock()
		info, exists := o.services[evt.Service]
		if exists && !staleLifecycleEvent(info, evt) {
			switch info.State {
			case StateStarting, StateHealthy:
				info.Transition(StateDegraded)
				info.StateReason = evt.Message
				info.FailureKind = evt.FailureKind
				var conflict *port.ConflictError
				if errors.As(evt.Err, &conflict) {
					info.PortConflict = conflict
					info.StateReason = conflict.Error()
				}
			case StateDegraded:
				if info.StateReason == DockerObservationUnavailableReason {
					break
				}
				// Recovery probing can produce a sharper diagnosis than
				// the original exhaustion message (e.g. zombie detection)
				// — refresh the reason without a state change.
				// Cancellation caused by a process exit is follow-on noise,
				// not better evidence than the exit already recorded.
				if evt.Message != "" && !(errors.Is(evt.Err, context.Canceled) && info.StateReason != "") {
					info.StateReason = evt.Message
					info.FailureKind = evt.FailureKind
				}
			}
		}
		o.mu.Unlock()

	case EventBuildStarted:
		o.mu.Lock()
		if info, exists := o.services[evt.Service]; exists && !staleLifecycleEvent(info, evt) && info.State == StateStarting {
			info.Transition(StateBuilding)
		}
		o.mu.Unlock()

	case EventBuildComplete:
		o.mu.Lock()
		if info, exists := o.services[evt.Service]; exists && !staleLifecycleEvent(info, evt) && info.State == StateBuilding {
			info.Transition(StateStarting)
		}
		o.mu.Unlock()

	case EventBuildFailed:
		o.mu.Lock()
		if info, exists := o.services[evt.Service]; exists && !staleLifecycleEvent(info, evt) && info.State == StateBuilding {
			info.Transition(StateDegraded)
			info.StateReason = "build failed"
			info.FailureKind = FailureKindBuild
		}
		o.mu.Unlock()

	case EventProcessExited:
		// An exit while Stopping/Stopped is the expected result of a stop
		// request (the guard below skips it — StopService already set the
		// final state). Any other exit is a crash: mark Degraded, per the
		// state machine in docs/architecture.md, so the dashboard/CLI show
		// a red "something died" rather than a gray "someone stopped it".
		o.mu.Lock()
		info, exists := o.services[evt.Service]
		if exists && !staleLifecycleEvent(info, evt) && info.State != StateStopping && info.State != StateStopped {
			info.Transition(StateDegraded)
			reason := evt.Message
			if reason == "" {
				reason = "process exited unexpectedly"
			}
			info.StateReason = reason
			info.FailureKind = FailureKindProcess
			info.FailureEvidence = evt.Evidence
			// The process is gone: any startup poll or recovery probing for
			// this generation is now pointless (nothing will start answering
			// on its own), and a live recovery loop would keep the service
			// looking "recovering" while probing a closed port forever.
			info.cancelLifecycle()
		}
		o.mu.Unlock()

	case EventContainerDrift:
		o.mu.Lock()
		info, exists := o.services[evt.Service]
		if !exists {
			o.mu.Unlock()
			break
		}
		prev := info.State
		gen := info.Generation
		info.Transition(StateDegraded)
		if evt.Message != "" {
			info.StateReason = evt.Message
		} else {
			info.StateReason = "container state drifted"
		}
		info.FailureKind = FailureKindRuntime
		o.mu.Unlock()

		// Re-check health — container may have recovered. NB: this runs on
		// the event-loop ctx, not the per-service lifecycle ctx (no
		// production sender emits EventContainerDrift today; if one appears,
		// pass info.ctx so stop/restart cancels the re-check).
		if prev == StateHealthy && o.OnHealthCheck != nil {
			_ = o.OnHealthCheck(ctx, evt.Service, gen)
		}

	case EventShutdown:
		// Handled by the caller (graceful shutdown)
		return nil
	}

	return nil
}

// staleLifecycleEvent reports whether a build, health, or process-exit event
// was produced by a different start of the service than the current one.
// Strict
// equality: every production sender stamps the generation it was started
// with, and generation 0 only matches services adopted without a start —
// daemon-reconnected processes and infra marked healthy in place. A
// gen-0 event can therefore never touch a service that has since been
// (re)started. Caller must hold o.mu.
func staleLifecycleEvent(info *ServiceInfo, evt Event) bool {
	return evt.Generation != info.Generation
}
