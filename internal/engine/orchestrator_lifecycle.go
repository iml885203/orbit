package engine

import (
	"context"
	"fmt"
)

func (o *Orchestrator) startService(ctx context.Context, name string) error {
	o.mu.Lock()
	info := o.services[name]
	if info.State == StateHealthy || info.State == StateStarting {
		o.mu.Unlock()
		return nil // already running or starting, skip
	}
	// Order matters: cancel any old lifecycle ctx before installing the
	// new ctx/cancel, then run Transition. If a future change makes
	// Transition(StateStarting) call cancelLifecycle (mirroring Stopping),
	// the freshly assigned cancel would be nilled — keep the assignments
	// after the explicit cancelLifecycle call above, never before.
	info.cancelLifecycle()
	svcCtx, cancel := context.WithCancel(ctx)
	info.ctx = svcCtx
	info.cancel = cancel
	info.Generation++
	gen := info.Generation
	info.Transition(StateStarting)
	o.mu.Unlock()

	go func() {
		// One snapshot per start: this generation launches with the config
		// published at its start time (a SQL-mode switch takes effect on
		// the next restart of the container — the correctness path the
		// holder refactor must preserve).
		cfg := o.holder.Load()
		var err error
		if info.Kind == "container" {
			if o.OnStartContainer != nil {
				err = o.OnStartContainer(svcCtx, name, cfg.Containers[name])
			}
		} else {
			if o.OnStartProcess != nil {
				err = o.OnStartProcess(svcCtx, name, gen, cfg, cfg.Services[name])
			}
		}

		if err != nil {
			o.events <- Event{
				Type:       EventHealthFail,
				Service:    name,
				Message:    fmt.Sprintf("failed to start: %v", err),
				Err:        err,
				Generation: gen,
			}
			return
		}

		// Start health checking on the per-service ctx so a stop during
		// startup cancels the health poll instead of leaking a goroutine.
		if o.OnHealthCheck != nil {
			_ = o.OnHealthCheck(svcCtx, name, gen)
		}
	}()

	return nil
}

// Start transitions the listed services from stopped/degraded into pending,
// re-computing their dependency wait set. Services already healthy/starting
// are skipped. Services not in the orchestrator (not in config) are ignored.
func (o *Orchestrator) Start(names []string) {
	// One snapshot for the whole Start operation — pending deps for every
	// requested service must come from the same config generation.
	cfg := o.holder.Load()
	o.mu.Lock()
	var ready []string
	for _, name := range names {
		info, exists := o.services[name]
		if !exists {
			continue
		}
		if info.State == StateHealthy || info.State == StateStarting || info.State == StatePending {
			continue
		}
		info.Transition(StatePending)
		info.PendingDeps = o.calcPendingDeps(cfg, name)
		if len(info.PendingDeps) == 0 {
			ready = append(ready, name)
		}
	}
	o.mu.Unlock()

	for _, name := range ready {
		o.events <- Event{Type: EventDepsReady, Service: name}
	}
}

// OnContainerSeen adopts externally-running containers and detects drift.
//   - stopped + running        → healthy (adoption)
//   - healthy + not running    → degraded (drift)
//   - other transitions are ignored; starting → healthy is driven by
//     the normal health-check path, not the poller.
//
// Unknown service names are silently ignored.
func (o *Orchestrator) OnContainerSeen(name string, running bool) {
	o.mu.Lock()
	info, exists := o.services[name]
	if !exists {
		o.mu.Unlock()
		return
	}
	prev := info.State
	switch {
	case running && prev == StateStopped:
		info.Transition(StateHealthy)
	case !running && prev == StateHealthy:
		// Docker reports the container as existing but not running while we
		// believe it healthy — it died outside orbit's control (crash, OOM
		// kill, manual docker stop).
		info.Transition(StateDegraded)
		info.StateReason = "container exited unexpectedly"
	}
	newState := info.State
	o.mu.Unlock()

	if prev != newState && newState == StateHealthy {
		o.notifyDependents(name)
	}
}

// OnContainerGone reconciles a tracked container that no longer exists in
// Docker at all (removed, not merely exited). Poller-driven, the removal
// counterpart of OnContainerSeen:
//   - stopping/degraded → stopped: a docker remove that outlives its stop
//     ctx (slow force-delete) still completes — without this the service
//     stays degraded forever and `orbit down` waits out its full timeout
//   - healthy → degraded (drift): the container was removed outside orbit
//   - other transitions are ignored: before/during startup the container
//     legitimately doesn't exist yet
//
// Unknown names and non-container services are silently ignored. When this
// races a StopService whose docker call is still in flight, both paths
// converge on stopped but EventProcessExited may be broadcast twice —
// acceptable, since lifecycle events are observational (see Subscribe's
// drop policy).
func (o *Orchestrator) OnContainerGone(name string) {
	o.mu.Lock()
	info, exists := o.services[name]
	if !exists || info.Kind != "container" {
		o.mu.Unlock()
		return
	}
	prev := info.State
	switch prev {
	case StateStopping:
		info.Transition(StateStopped)
	case StateDegraded:
		if info.AwaitingContainerRemoval {
			info.Transition(StateStopped)
		}
	case StateHealthy:
		info.Transition(StateDegraded)
		info.StateReason = "container removed outside orbit"
	}
	newState := info.State
	o.mu.Unlock()

	if prev != newState && newState == StateStopped {
		o.broadcast(Event{Type: EventProcessExited, Service: name, Message: "stopped"})
	}
}

// StopService sets a service to Stopping, calls the appropriate stop callback,
// then sets it to Stopped.
func (o *Orchestrator) StopService(ctx context.Context, name string) error {
	o.mu.Lock()
	info, exists := o.services[name]
	if !exists {
		o.mu.Unlock()
		return fmt.Errorf("unknown service: %s", name)
	}
	// Transition cancels the lifecycle ctx so health checks and wait
	// strategies bail out, and no new child is spawned mid-stop.
	info.Transition(StateStopping)
	o.mu.Unlock()

	o.narrate(name, "stop requested")

	var err error
	if info.Kind == "container" {
		if o.OnStopContainer != nil {
			err = o.OnStopContainer(ctx, name)
		}
	} else {
		if o.OnStopProcess != nil {
			err = o.OnStopProcess(name)
		}
	}

	o.mu.Lock()
	if err != nil {
		info.Transition(StateDegraded)
		info.StateReason = fmt.Sprintf("stop failed: %v", err)
		info.AwaitingContainerRemoval = info.Kind == "container"
	} else {
		info.Transition(StateStopped)
	}
	o.mu.Unlock()

	if err != nil {
		o.broadcast(Event{Type: EventHealthFail, Service: name, Message: fmt.Sprintf("stop failed: %v", err)})
		return err
	}
	o.broadcast(Event{Type: EventProcessExited, Service: name, Message: "stopped"})
	return nil
}

// RestartService stops and restarts a service.
func (o *Orchestrator) RestartService(ctx context.Context, name string) error {
	o.mu.RLock()
	info, exists := o.services[name]
	if !exists {
		o.mu.RUnlock()
		return fmt.Errorf("unknown service: %s", name)
	}
	kind := info.Kind
	o.mu.RUnlock()

	o.narrate(name, "restart requested")

	o.mu.Lock()
	// Stopping (not just a ctx cancel) so the poller's OnContainerGone
	// reads the container vanishing as a stop in progress, not as
	// healthy-drift ("removed outside orbit"). Transition(StateStopping)
	// also cancels the lifecycle ctx.
	o.services[name].Transition(StateStopping)
	// Invalidate exit/health events from the process being replaced before
	// Stop can unblock. Restart moves through Pending rather than Stopped, so
	// state alone cannot distinguish a late expected exit from a new crash.
	o.services[name].Generation++
	o.mu.Unlock()

	// Stop
	var stopErr error
	if kind == "container" {
		if o.OnStopContainer != nil {
			stopErr = o.OnStopContainer(ctx, name)
		}
	} else {
		if o.OnStopProcess != nil {
			stopErr = o.OnStopProcess(name)
		}
	}
	if stopErr != nil {
		o.narrate(name, "stop failed: "+stopErr.Error())
		o.mu.Lock()
		info = o.services[name]
		info.Transition(StateDegraded)
		info.StateReason = fmt.Sprintf("could not restart: failed to stop existing resource: %v", stopErr)
		info.AwaitingContainerRemoval = kind == "container"
		o.mu.Unlock()
		o.broadcast(Event{
			Type:    EventHealthFail,
			Service: name,
			Message: fmt.Sprintf("could not restart: failed to stop existing resource: %v", stopErr),
		})
		return stopErr
	}

	o.mu.Lock()
	info = o.services[name]
	info.Transition(StatePending)
	info.PendingDeps = o.calcPendingDeps(o.holder.Load(), name)
	info.MarkRestart()
	ready := len(info.PendingDeps) == 0
	o.mu.Unlock()

	if ready {
		o.events <- Event{Type: EventDepsReady, Service: name}
	}
	return nil
}

// MarkServiceHealthy sets a service state to Healthy and notifies dependents.
// Used for reconnecting to already-running services.
func (o *Orchestrator) MarkServiceHealthy(name string) {
	o.mu.Lock()
	info, exists := o.services[name]
	if exists {
		info.Transition(StateHealthy)
	}
	o.mu.Unlock()

	if exists {
		o.notifyDependents(name)
	}
}

// SetServiceKind updates the Kind of a service (used for dev/container mode toggle).
func (o *Orchestrator) SetServiceKind(name, kind string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if info, exists := o.services[name]; exists {
		info.Kind = kind
	}
}

// MarkServiceStopped marks a service as stopped without calling stop callbacks.
func (o *Orchestrator) MarkServiceStopped(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if info, exists := o.services[name]; exists {
		info.Transition(StateStopped)
	}
}
