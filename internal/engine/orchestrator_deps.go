package engine

import (
	"github.com/iml885203/orbit/internal/config"
)

// calcPendingDeps returns the set of dependencies not yet healthy for a service.
// Must be called with o.mu held.
func (o *Orchestrator) calcPendingDeps(_ *config.Config, name string) map[string]bool {
	pending := make(map[string]bool)
	g := o.DepGraph()
	for _, dep := range g.DepsOf(name) {
		if depInfo, ok := o.services[dep]; ok && depInfo.State == StateHealthy {
			continue
		}
		pending[dep] = true
	}
	return pending
}

func (o *Orchestrator) notifyDependents(readyService string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	for name, info := range o.services {
		if info.State == StatePending && info.MarkDependencyReady(readyService) {
			evt := Event{Type: EventDepsReady, Service: name, pendingStart: info.pendingStart}
			go func() {
				o.events <- evt
			}()
		}
	}
}
