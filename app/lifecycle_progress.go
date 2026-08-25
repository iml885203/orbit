package app

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/iml885203/orbit/daemon"
)

type lifecyclePhase string

const (
	phaseEnsuringDaemon            lifecyclePhase = "ensuring daemon"
	phaseCheckingEnvironment       lifecyclePhase = "checking environment"
	phaseApplyingEnvironment       lifecyclePhase = "applying environment"
	phaseRequestingResourceStart   lifecyclePhase = "requesting resource start"
	phaseRequestingResourceRestore lifecyclePhase = "requesting resource restore"
	phaseWaitingForReadiness       lifecyclePhase = "waiting for readiness"
	phaseCollectingFailureEvidence lifecyclePhase = "collecting failure evidence"

	progressCheckInterval = 500 * time.Millisecond
)

type lifecycleProgress struct {
	ctx      context.Context
	out      io.Writer
	started  time.Time
	interval time.Duration
	ticks    <-chan time.Time
	stopTick func()

	mu                 sync.Mutex
	phase              lifecyclePhase
	phaseSince         time.Time
	lastPhaseHeartbeat time.Time
	resources          map[string]progressSnapshot

	done     chan struct{}
	finished chan struct{}
	close    sync.Once
}

func newLifecycleProgress(ctx context.Context, out io.Writer, started time.Time) *lifecycleProgress {
	ticker := time.NewTicker(progressCheckInterval)
	return newLifecycleProgressWithTicker(ctx, out, started, heartbeatInterval, ticker.C, ticker.Stop)
}

func newLifecycleProgressWithTicker(
	ctx context.Context,
	out io.Writer,
	started time.Time,
	interval time.Duration,
	ticks <-chan time.Time,
	stopTick func(),
) *lifecycleProgress {
	p := &lifecycleProgress{
		ctx:       ctx,
		out:       out,
		started:   started,
		interval:  interval,
		ticks:     ticks,
		stopTick:  stopTick,
		resources: map[string]progressSnapshot{},
		done:      make(chan struct{}),
		finished:  make(chan struct{}),
	}
	go p.run()
	return p
}

func (p *lifecycleProgress) run() {
	defer close(p.finished)
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.done:
			return
		case now := <-p.ticks:
			p.heartbeat(now)
		}
	}
}

func (p *lifecycleProgress) Phase(phase lifecyclePhase) {
	p.phaseAt(phase, time.Now())
}

func (p *lifecycleProgress) phaseAt(phase lifecyclePhase, now time.Time) {
	if p == nil || p.ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.phase == phase {
		return
	}
	p.phase = phase
	p.phaseSince = now
	p.lastPhaseHeartbeat = time.Time{}
	p.resources = map[string]progressSnapshot{}
	_, _ = fmt.Fprintf(p.out, "⋯ %s\n", phase)
}

func (p *lifecycleProgress) ObserveResources(resources []daemon.ResourceStatus, now time.Time) {
	if p == nil || p.ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resources = nextSnapshots(p.resources, resources, now)
}

func (p *lifecycleProgress) heartbeat(now time.Time) {
	if p.ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.phase == "" || now.Sub(maxTime(p.phaseSince, p.lastPhaseHeartbeat)) < p.interval {
		return
	}

	emittedResource := false
	for _, event := range diffProgressAtInterval(p.resources, p.resources, now, p.interval) {
		if event.kind != eventHeartbeat {
			continue
		}
		snapshot := p.resources[event.name]
		snapshot.lastHeartbeat = now
		p.resources[event.name] = snapshot
		_, _ = fmt.Fprintf(p.out, "⋯ %s still %s%s\n", event.name, event.to, p.timing(now, event.elapsed))
		emittedResource = true
	}
	if emittedResource {
		p.lastPhaseHeartbeat = now
		return
	}
	if p.phase == phaseWaitingForReadiness && p.resourceHeartbeatDueBy(now.Add(progressCheckInterval)) {
		return
	}

	_, _ = fmt.Fprintf(p.out, "⋯ %s%s\n", p.phase, p.timing(now, now.Sub(p.started)))
	p.lastPhaseHeartbeat = now
}

func (p *lifecycleProgress) resourceHeartbeatDueBy(deadline time.Time) bool {
	for name := range p.resources {
		snapshot := p.resources[name]
		if heartbeatable[snapshot.state] && deadline.Sub(maxTime(snapshot.since, snapshot.lastHeartbeat)) >= p.interval {
			return true
		}
	}
	return false
}

func (p *lifecycleProgress) timing(now time.Time, elapsed time.Duration) string {
	remaining := ""
	if deadline, ok := p.ctx.Deadline(); ok {
		budget := deadline.Sub(now)
		if budget < 0 {
			budget = 0
		}
		remaining = ", about " + fmtDur(budget) + " remaining"
	}
	return fmt.Sprintf(" (elapsed %s%s)", fmtDur(elapsed), remaining)
}

func (p *lifecycleProgress) Close() {
	if p == nil {
		return
	}
	p.close.Do(func() {
		if p.stopTick != nil {
			p.stopTick()
		}
		close(p.done)
		<-p.finished
	})
}
