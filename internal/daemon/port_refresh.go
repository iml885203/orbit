package daemon

import (
	"log/slog"
	"maps"
	"slices"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/port"
)

// refreshStaleAutoPorts republishes the config with fresh selections for
// movable ports that went stale while the daemon was alive: a preferred
// port grabbed by another process after daemon startup should relocate on
// the next start — the same behavior a fresh daemon would give — instead
// of surfacing as a conflict error. Active resources keep their ports and
// fixed ports keep their hard-conflict semantics. A refresh failure never
// blocks the start; the existing selections surface their own conflict
// evidence.
func (s *Server) refreshStaleAutoPorts() {
	active := map[string]bool{}
	for _, svc := range s.app.Orchestrator.GetAllServices() {
		switch svc.State {
		case engine.StateStopped, engine.StatePending:
		default:
			active[svc.Name] = true
		}
	}
	err := s.UpdateConfig(func(tx extension.ConfigTx) error {
		cfg := clonePortSurface(tx.Current())
		resolutions, err := port.RefreshStaleAutoPorts(cfg, func(name string) bool { return active[name] }, DashboardPort())
		if err != nil {
			return err
		}
		if len(resolutions) == 0 {
			return nil
		}
		for _, r := range resolutions {
			slog.Info(
				"reselected stale port",
				"component", "orbit",
				"name", r.Resource,
				"label", r.Label,
				"previous", r.Preferred,
				"actual", r.Actual,
			)
		}
		tx.Store(cfg)
		return nil
	})
	if err != nil {
		slog.Warn("stale-port refresh failed; starting with existing selections", "component", "orbit", "err", err)
	}
}

// clonePortSurface copies the config down to every field the port
// resolver mutates (port maps, health checks, service URLs), so a refresh
// edits a private copy and publishes it atomically instead of writing
// into the immutable snapshot other readers hold.
func clonePortSurface(cfg *config.Config) *config.Config {
	next := *cfg
	next.Containers = make(map[string]*config.Container, len(cfg.Containers))
	for name, container := range cfg.Containers {
		clone := *container
		clone.Ports = maps.Clone(container.Ports)
		if container.HealthCheck != nil {
			check := *container.HealthCheck
			clone.HealthCheck = &check
		}
		clone.Sidecars = slices.Clone(container.Sidecars)
		for i := range clone.Sidecars {
			clone.Sidecars[i].Ports = maps.Clone(clone.Sidecars[i].Ports)
		}
		next.Containers[name] = &clone
	}
	next.Services = make(map[string]*config.Service, len(cfg.Services))
	for name, service := range cfg.Services {
		clone := *service
		clone.Ports = maps.Clone(service.Ports)
		if service.HealthCheck != nil {
			check := *service.HealthCheck
			clone.HealthCheck = &check
		}
		next.Services[name] = &clone
	}
	return &next
}
