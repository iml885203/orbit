package engine

import "github.com/iml885203/orbit/config"

// FilterEnabledServices returns the set of service/container names that should
// be started based on the enabled groups. If no groups are defined, all
// services and containers are included.
func FilterEnabledServices(cfg *config.Config, groupOverrides []string) map[string]bool {
	return FilterEnabledServicesWithDetached(cfg, groupOverrides, nil)
}

// FilterEnabledServicesWithDetached is FilterEnabledServices plus an overlay
// of detached edges (keyed by "from" service, value = list of "to" deps to
// skip). Used by the daemon at request time so `orbit up <service>` doesn't
// drag in a detached dependency.
func FilterEnabledServicesWithDetached(cfg *config.Config, groupOverrides []string, detached map[string][]string) map[string]bool {
	enabled := make(map[string]bool)

	switch {
	case len(groupOverrides) > 0:
		groupSet := make(map[string]bool, len(groupOverrides))
		for _, g := range groupOverrides {
			groupSet[g] = true
		}
		for name, grp := range cfg.Groups {
			if groupSet[name] {
				for _, svc := range grp.Services {
					enabled[svc] = true
				}
			}
		}
	case len(cfg.Groups) > 0:
		for _, grp := range cfg.Groups {
			if grp.Enabled {
				for _, svc := range grp.Services {
					enabled[svc] = true
				}
			}
		}
	default:
		for name := range cfg.Services {
			enabled[name] = true
		}
	}

	// Always include all containers (infrastructure)
	for name := range cfg.Containers {
		enabled[name] = true
	}

	AddDepsWithDetached(cfg, enabled, detached)
	return enabled
}

// AddDeps walks the dependency tree and adds every transitive dependency of
// the entries already in `enabled` to the same set.
func AddDeps(cfg *config.Config, enabled map[string]bool) {
	AddDepsWithDetached(cfg, enabled, nil)
}

// AddDepsWithDetached is AddDeps but skips edges listed in detached.
// detached is keyed by the "from" service name.
func AddDepsWithDetached(cfg *config.Config, enabled map[string]bool, detached map[string][]string) {
	g := NewDepGraph(cfg, detached)
	changed := true
	for changed {
		changed = false
		for name := range enabled {
			for _, dep := range g.DepsOf(name) {
				if !enabled[dep] {
					enabled[dep] = true
					changed = true
				}
			}
		}
	}
}
