package container

import (
	"context"
	"log/slog"
	"time"

	"github.com/iml885203/orbit/dockerctx"
	"github.com/moby/moby/client"
)

// ContainerState represents a snapshot of a container's Docker state.
type ContainerState struct {
	Name    string
	Status  string // running, exited, etc.
	Running bool
}

// Poller periodically polls Docker for the state of orbit-managed containers
// in a given namespace and reports drift via a callback.
type Poller struct {
	cli       *client.Client
	interval  time.Duration
	namespace string

	// OnDrift is called when a container's state changes unexpectedly.
	OnDrift func(name string, state ContainerState)
	// OnStateUpdate is called on every poll with current states.
	OnStateUpdate func(states map[string]ContainerState)

	lastStates map[string]ContainerState
}

// NewPoller creates a container state poller for the given namespace.
// Pass "" for the default (un-namespaced) instance.
func NewPoller(cli *client.Client, namespace string, interval time.Duration) *Poller {
	if cli == nil {
		c, err := dockerctx.NewClient()
		if err != nil {
			slog.Error("failed to create Docker client", "component", "poller", "err", err)
		}
		cli = c
	}
	return &Poller{
		cli:        cli,
		interval:   interval,
		namespace:  namespace,
		lastStates: make(map[string]ContainerState),
	}
}

// Run starts polling. Blocks until context is cancelled.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	containers, err := p.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", labelManaged+"=true"),
	})
	if err != nil {
		slog.Error("Docker list error", "component", "poller", "err", err)
		return
	}

	current := make(map[string]ContainerState)
	for i := range containers.Items {
		if containers.Items[i].Labels[labelNamespace] != p.namespace {
			continue
		}
		name := containers.Items[i].Labels[labelService]
		if name == "" {
			continue
		}
		state := ContainerState{
			Name:    name,
			Status:  string(containers.Items[i].State),
			Running: containers.Items[i].State == "running",
		}
		current[name] = state

		// Detect drift: was running, now not
		if prev, existed := p.lastStates[name]; existed {
			if prev.Running && !state.Running {
				if p.OnDrift != nil {
					p.OnDrift(name, state)
				}
			}
		}
	}

	if p.OnStateUpdate != nil {
		p.OnStateUpdate(current)
	}

	p.lastStates = current
}
