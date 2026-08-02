package engine

import (
	"context"
	"log/slog"
	"sync"

	"github.com/iml885203/orbit/config"
)

// Orchestrator is the central event loop that coordinates all services.
type Orchestrator struct {
	// holder is the shared immutable-config publisher. Operations Load()
	// one snapshot each (per start, per event). Config reconciliation swaps
	// the services map and DepGraph under their owning locks with the holder.
	holder     *config.Holder
	services   map[string]*ServiceInfo
	events     chan Event
	depGraph   *DepGraph
	depGraphMu sync.RWMutex
	mu         sync.RWMutex

	// Callbacks set by the application
	OnStartContainer func(ctx context.Context, name string, cfg *config.Container) error
	OnStopContainer  func(ctx context.Context, name string) error
	// OnStartProcess receives the per-start config snapshot alongside the
	// service definition drawn from it, so the callback never Loads a
	// second (possibly newer) snapshot mid-start.
	OnStartProcess func(ctx context.Context, name string, generation int, cfg *config.Config, svc *config.Service) error
	OnStopProcess  func(name string) error
	OnHealthCheck  func(ctx context.Context, name string, generation int) error
	OnRunInit      func(ctx context.Context, name string, cfg *config.Container) error
	// OnAction narrates lifecycle actions into per-service log buffers.
	OnAction func(name string, msg string)

	// Subscribers receive copies of events (for logging, state persistence, etc.)
	subscribers map[int]chan Event
	nextSubID   int
	subMu       sync.Mutex
}

// NewOrchestrator enumerates every service defined in cfg into StateStopped.
// Nothing runs until Start(names) is called or OnContainerSeen adopts it.
// serviceModes overrides the default kind for dual-defined names
// (e.g., "api": "container").
// detachedDeps is a map from service name to the list of dependency names that
// should be treated as detached (i.e., ignored for startup ordering). Pass nil
// if no edges are detached.
func NewOrchestrator(holder *config.Holder, serviceModes map[string]string, detachedDeps map[string][]string) *Orchestrator {
	cfg := holder.Load()
	o := &Orchestrator{
		holder:      holder,
		services:    make(map[string]*ServiceInfo),
		events:      make(chan Event, 256),
		subscribers: make(map[int]chan Event),
		depGraph:    NewDepGraph(cfg, detachedDeps),
	}

	for name := range cfg.Containers {
		if _, dualDefined := cfg.Services[name]; dualDefined {
			continue // handled below, where serviceModes decides the kind
		}
		o.services[name] = &ServiceInfo{
			Name:  name,
			Kind:  "container",
			State: StateStopped,
		}
	}
	for name := range cfg.Services {
		kind := "service"
		if serviceModes[name] == "container" {
			kind = "container"
		}
		o.services[name] = &ServiceInfo{
			Name:  name,
			Kind:  kind,
			State: StateStopped,
		}
	}

	return o
}

// Events returns the event channel for sending events.
func (o *Orchestrator) Events() chan<- Event {
	return o.events
}

func (o *Orchestrator) narrate(name, msg string) {
	if o.OnAction != nil {
		o.OnAction(name, msg)
	}
}

// Subscribe returns a channel that receives copies of all events, plus an
// unsubscribe function. The unsubscribe closes the channel and removes it
// from the subscriber set; calling unsubscribe more than once is a no-op.
func (o *Orchestrator) Subscribe() (chan Event, func()) {
	ch := make(chan Event, 64)
	o.subMu.Lock()
	id := o.nextSubID
	o.nextSubID++
	o.subscribers[id] = ch
	o.subMu.Unlock()
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			o.subMu.Lock()
			if _, ok := o.subscribers[id]; ok {
				delete(o.subscribers, id)
				close(ch)
			}
			o.subMu.Unlock()
		})
	}
	return ch, unsubscribe
}

// GetServiceInfo returns a snapshot of service info.
func (o *Orchestrator) GetServiceInfo(name string) (ServiceInfo, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	info, ok := o.services[name]
	if !ok {
		return ServiceInfo{}, false
	}
	return *info, true
}

// UpdateDetachedDeps replaces the orchestrator's detached-deps map with the
// provided snapshot. This allows the daemon to propagate UI detach/reattach
// actions to the already-running orchestrator without a restart.
// detached is keyed by "from" service name; values list the suppressed "to" deps.
func (o *Orchestrator) UpdateDetachedDeps(detached map[string][]string) {
	o.depGraphMu.Lock()
	o.depGraph = NewDepGraph(o.holder.Load(), detached)
	o.depGraphMu.Unlock()
}

// DepGraph returns the current DepGraph under a reader lock.
func (o *Orchestrator) DepGraph() *DepGraph {
	o.depGraphMu.RLock()
	defer o.depGraphMu.RUnlock()
	return o.depGraph
}

// GetAllServices returns a snapshot of all service infos.
func (o *Orchestrator) GetAllServices() []ServiceInfo {
	o.mu.RLock()
	defer o.mu.RUnlock()
	result := make([]ServiceInfo, 0, len(o.services))
	for _, info := range o.services {
		result = append(result, *info)
	}
	return result
}

// Run drives the event loop. It does NOT auto-start anything — services stay
// in StateStopped until Start(names) is called or OnContainerSeen adopts
// a running container. Blocks until context is cancelled.
func (o *Orchestrator) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt := <-o.events:
			o.broadcast(evt)
			if err := o.handleEvent(ctx, evt); err != nil {
				slog.Error("error handling event", "component", "orchestrator", "type", evt.Type, "service", evt.Service, "err", err)
			}
		}
	}
}

func (o *Orchestrator) broadcast(evt Event) {
	o.subMu.Lock()
	defer o.subMu.Unlock()
	for _, ch := range o.subscribers {
		select {
		case ch <- evt:
		default:
			// drop if subscriber is slow
		}
	}
}
