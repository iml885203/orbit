package engine

import (
	"reflect"
	"sort"

	"github.com/iml885203/orbit/config"
)

type ConfigReconcilePlan struct {
	RestartRequired bool
	Stop            []string
	Restart         []string
	Removed         []string
}

func PlanConfigReconcile(oldCfg, newCfg *config.Config, running map[string]bool, serviceModes map[string]string) ConfigReconcilePlan {
	plan := ConfigReconcilePlan{}
	if !reflect.DeepEqual(oldCfg.Settings, newCfg.Settings) ||
		!reflect.DeepEqual(oldCfg.Tracing, newCfg.Tracing) ||
		!reflect.DeepEqual(oldCfg.Extensions, newCfg.Extensions) {
		plan.RestartRequired = true
		return plan
	}

	changed := changedRuntimeResources(oldCfg, newCfg, serviceModes)
	removed := removedRuntimeResources(oldCfg, newCfg)
	impacted := transitiveDependents(changed, oldCfg, newCfg)
	for name := range removed {
		impacted[name] = true
	}

	for name := range impacted {
		if !running[name] {
			continue
		}
		plan.Stop = append(plan.Stop, name)
		if newCfg.ServiceOrContainerExists(name) {
			plan.Restart = append(plan.Restart, name)
		}
	}
	for name := range removed {
		plan.Removed = append(plan.Removed, name)
	}
	sort.Strings(plan.Stop)
	sort.Strings(plan.Restart)
	sort.Strings(plan.Removed)
	return plan
}

func changedRuntimeResources(oldCfg, newCfg *config.Config, serviceModes map[string]string) map[string]bool {
	changed := make(map[string]bool)
	for _, name := range runtimeResourceNames(oldCfg) {
		if !newCfg.ServiceOrContainerExists(name) {
			continue
		}
		if !reflect.DeepEqual(
			runtimeResourceDefinition(oldCfg, name, serviceModes),
			runtimeResourceDefinition(newCfg, name, serviceModes),
		) {
			changed[name] = true
		}
	}
	return changed
}

func runtimeResourceDefinition(cfg *config.Config, name string, serviceModes map[string]string) any {
	service, hasService := cfg.Services[name]
	container, hasContainer := cfg.Containers[name]
	if hasService && (!hasContainer || serviceModes[name] != "container") {
		return service
	}
	return container
}

func removedRuntimeResources(oldCfg, newCfg *config.Config) map[string]bool {
	removed := make(map[string]bool)
	for name := range oldCfg.Containers {
		if !newCfg.ServiceOrContainerExists(name) {
			removed[name] = true
		}
	}
	for name := range oldCfg.Services {
		if !newCfg.ServiceOrContainerExists(name) {
			removed[name] = true
		}
	}
	return removed
}

func transitiveDependents(seeds map[string]bool, configs ...*config.Config) map[string]bool {
	impacted := make(map[string]bool, len(seeds))
	for name := range seeds {
		impacted[name] = true
	}
	for changed := true; changed; {
		changed = false
		for _, cfg := range configs {
			for _, name := range runtimeResourceNames(cfg) {
				if impacted[name] {
					continue
				}
				for _, dependency := range cfg.GetDependencies(name) {
					if impacted[dependency] {
						impacted[name] = true
						changed = true
						break
					}
				}
			}
		}
	}
	return impacted
}

func runtimeResourceNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Containers)+len(cfg.Services))
	for name := range cfg.Containers {
		names = append(names, name)
	}
	for name := range cfg.Services {
		if _, duplicate := cfg.Containers[name]; !duplicate {
			names = append(names, name)
		}
	}
	return names
}

func (o *Orchestrator) ApplyConfig(cfg *config.Config, detached map[string][]string, serviceModes map[string]string) {
	o.mu.Lock()
	o.depGraphMu.Lock()

	nextServices := make(map[string]*ServiceInfo, len(cfg.Containers)+len(cfg.Services))
	for name := range cfg.Containers {
		if _, dualDefined := cfg.Services[name]; dualDefined {
			continue
		}
		nextServices[name] = preservedServiceInfo(o.services[name], name, "container")
	}
	for name := range cfg.Services {
		kind := "service"
		if serviceModes[name] == "container" {
			kind = "container"
		}
		nextServices[name] = preservedServiceInfo(o.services[name], name, kind)
	}

	o.holder.Store(cfg)
	o.services = nextServices
	o.depGraph = NewDepGraph(cfg, detached)
	o.depGraphMu.Unlock()
	o.mu.Unlock()
}

func preservedServiceInfo(current *ServiceInfo, name, kind string) *ServiceInfo {
	if current != nil && current.Kind == kind {
		return current
	}
	return &ServiceInfo{Name: name, Kind: kind, State: StateStopped}
}
