package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/iml885203/orbit/internal/dockerctx"
	"github.com/moby/moby/client"
)

const (
	dockerObservationRequestTimeout = 2 * time.Second
	dockerObservationCycleTimeout   = 10 * time.Second
)

// ContainerState represents a snapshot of a container's Docker state.
type ContainerState struct {
	Name      string
	ID        string
	Status    string // running, exited, etc.
	Running   bool
	StartedAt time.Time
	// ExitCode and OOMKilled describe the last runtime once it has ended;
	// Docker leaves both zero for a container that never started.
	ExitCode  int
	OOMKilled bool
}

// Poller periodically polls Docker for the state of orbit-managed containers
// in a given namespace and reports drift via a callback.
type Poller struct {
	cli            *client.Client
	interval       time.Duration
	namespace      string
	requestTimeout time.Duration
	cycleTimeout   time.Duration

	// OnDrift is called when a container's state changes unexpectedly.
	OnDrift func(name string, state ContainerState)
	// OnStateUpdate is called after a complete successful poll with the states Docker listed at
	// listedAt. The list is taken before the per-container inspects, so the
	// states can be older than lifecycle changes made while the poll ran.
	OnStateUpdate func(listedAt time.Time, states map[string]ContainerState)
	// OnUnavailable is called once after Docker cannot be observed for two
	// consecutive polls. One missed poll is treated as transient transport
	// noise; a successful poll arms the callback for a future outage.
	OnUnavailable func(err error)

	lastStates          map[string]ContainerState
	consecutiveFailures int
	unavailable         bool
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
		cli:            cli,
		interval:       interval,
		namespace:      namespace,
		lastStates:     make(map[string]ContainerState),
		requestTimeout: dockerObservationRequestTimeout,
		cycleTimeout:   dockerObservationCycleTimeout,
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
	if p.cli == nil {
		p.observationFailed(ctx, errors.New("docker client is unavailable"))
		return
	}
	cycleCtx, cancelCycle := context.WithTimeout(ctx, p.cycleTimeout)
	defer cancelCycle()
	listedAt := time.Now()
	listCtx, cancelList := context.WithTimeout(cycleCtx, p.requestTimeout)
	containers, err := p.cli.ContainerList(listCtx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", labelManaged+"=true"),
	})
	cancelList()
	if err != nil {
		p.observationFailed(ctx, fmt.Errorf("listing containers: %w", err))
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
			ID:      containers.Items[i].ID,
			Status:  string(containers.Items[i].State),
			Running: containers.Items[i].State == "running",
		}
		inspectCtx, cancelInspect := context.WithTimeout(cycleCtx, p.requestTimeout)
		inspect, inspectErr := p.cli.ContainerInspect(
			inspectCtx,
			containers.Items[i].ID,
			client.ContainerInspectOptions{},
		)
		cancelInspect()
		if inspectErr != nil {
			p.observationFailed(ctx, fmt.Errorf("inspecting container %q: %w", name, inspectErr))
			return
		} else if inspect.Container.State != nil {
			// ContainerList and ContainerInspect are separate Docker API calls.
			// A container can move from created to running between them, so the
			// later inspect result is the authoritative runtime snapshot.
			state.Status = string(inspect.Container.State.Status)
			state.Running = inspect.Container.State.Running
			state.ExitCode = inspect.Container.State.ExitCode
			state.OOMKilled = inspect.Container.State.OOMKilled
			startedAt, parseErr := time.Parse(time.RFC3339Nano, inspect.Container.State.StartedAt)
			if parseErr == nil {
				state.StartedAt = startedAt
			}
		}
		current[name] = state
	}
	if err := cycleCtx.Err(); err != nil {
		p.observationFailed(ctx, err)
		return
	}
	if p.unavailable {
		slog.Info("Docker connection restored", "component", "poller")
	}
	p.consecutiveFailures = 0
	p.unavailable = false
	for name, state := range current {
		if prev, existed := p.lastStates[name]; existed {
			if prev.Running && !state.Running {
				if p.OnDrift != nil {
					p.OnDrift(name, state)
				}
			}
		}
	}

	if p.OnStateUpdate != nil {
		p.OnStateUpdate(listedAt, current)
	}

	p.lastStates = current
}

func (p *Poller) observationFailed(parent context.Context, err error) {
	if parent.Err() == nil {
		p.recordFailure(err)
	}
}

func (p *Poller) recordFailure(err error) {
	p.consecutiveFailures++
	if p.consecutiveFailures == 1 {
		slog.Warn("Docker poll failed; retrying", "component", "poller", "err", err)
	}
	if p.consecutiveFailures < 2 || p.unavailable {
		return
	}
	p.unavailable = true
	slog.Error("Docker is unavailable", "component", "poller", "err", err)
	if p.OnUnavailable != nil {
		p.OnUnavailable(err)
	}
}
