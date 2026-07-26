package container

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/moby/client"
)

// execPollInterval paces the ContainerExecInspect loop — Docker offers no
// blocking "wait for exec" primitive so we poll, but at 50ms per check
// rather than as fast as the socket can round-trip.
const execPollInterval = 50 * time.Millisecond

// ExecInContainer runs cmd inside the managed container for service, blocking
// until the command exits. Returns the exit code.
func (m *Manager) ExecInContainer(ctx context.Context, service string, cmd []string) (int, error) {
	name := m.ContainerName(service)

	created, err := m.cli.ExecCreate(ctx, name, client.ExecCreateOptions{
		Cmd: cmd,
	})
	if err != nil {
		return -1, fmt.Errorf("exec create %s: %w", name, err)
	}

	if _, err := m.cli.ExecStart(ctx, created.ID, client.ExecStartOptions{}); err != nil {
		return -1, fmt.Errorf("exec start %s: %w", name, err)
	}

	for {
		inspect, err := m.cli.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
		if err != nil {
			return -1, fmt.Errorf("exec inspect %s: %w", name, err)
		}
		if !inspect.Running {
			return inspect.ExitCode, nil
		}
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-time.After(execPollInterval):
		}
	}
}

// HealthStatus returns the Docker-reported Health.Status for service's
// container. Empty string when the image declares no HEALTHCHECK.
func (m *Manager) HealthStatus(ctx context.Context, service string) (string, error) {
	name := m.ContainerName(service)
	info, err := m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", name, err)
	}
	if info.Container.State == nil || info.Container.State.Health == nil {
		return "", nil
	}
	return string(info.Container.State.Health.Status), nil
}
