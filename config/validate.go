package config

import (
	"fmt"
	"strings"
)

// Validate checks config for errors: missing refs, cycles, port conflicts.
func Validate(cfg *Config) error {
	var errs []string

	known := make(map[string]bool)
	for name := range cfg.Containers {
		known[name] = true
	}
	for name := range cfg.Services {
		// Same name in both containers + services is allowed (dev/container mode toggle)
		known[name] = true
	}

	if sp := cfg.SQLProjects; sp != nil {
		if sp.Target == "" {
			errs = append(errs, "sql_projects.target is required")
		} else if _, ok := cfg.Containers[sp.Target]; !ok {
			errs = append(errs, fmt.Sprintf("sql_projects.target %q is not a declared container", sp.Target))
		}
	}

	// Check dependency references exist
	for name, s := range cfg.Services {
		for _, dep := range s.DependsOn {
			if !known[dep] {
				errs = append(errs, fmt.Sprintf("service %q depends on unknown %q", name, dep))
			}
		}
	}
	for name, c := range cfg.Containers {
		if !isValidPullPolicy(c.PullPolicy) {
			errs = append(errs, fmt.Sprintf("container %q has invalid pull_policy %q (expected always, if_not_present, or never)", name, c.PullPolicy))
		}
		for _, dep := range c.DependsOn {
			if !known[dep] {
				errs = append(errs, fmt.Sprintf("container %q depends on unknown %q", name, dep))
			}
		}
		for _, sc := range c.Sidecars {
			if !isValidPullPolicy(sc.PullPolicy) {
				errs = append(errs, fmt.Sprintf("sidecar %q/%q has invalid pull_policy %q (expected always, if_not_present, or never)", name, sc.Name, sc.PullPolicy))
			}
		}
	}

	// Check group service references
	for gname, g := range cfg.Groups {
		for _, svc := range g.Services {
			if !known[svc] {
				errs = append(errs, fmt.Sprintf("group %q references unknown service %q", gname, svc))
			}
		}
	}

	// External names share the node-id namespace with services and
	// containers; a collision would break edge lookups in the UI.
	for ename, ext := range cfg.Externals {
		if ext == nil {
			continue
		}
		if _, ok := cfg.Services[ename]; ok {
			errs = append(errs, fmt.Sprintf("external %q collides with service of the same name", ename))
		}
		if _, ok := cfg.Containers[ename]; ok {
			errs = append(errs, fmt.Sprintf("external %q collides with container of the same name", ename))
		}
		if len(ext.Kafka.Produces) == 0 && len(ext.Kafka.Consumes) == 0 {
			errs = append(errs, fmt.Sprintf("external %q declares no kafka topics; remove it or add produces/consumes", ename))
		}
	}

	// SQL Server tooling (sqlcmd/sqlpackage exec, env injection, seeding)
	// authenticates with the container's SA_PASSWORD; a missing password
	// used to fall back to a baked-in team default and fail late with an
	// auth error. Require it up front for every container those paths
	// treat as SQL Server: the canonical sql-server name, plus anything
	// whose image the env injector sniffs as MSSQL.
	for name, c := range cfg.Containers {
		if c == nil {
			continue
		}
		looksLikeMSSQL := name == "sql-server" ||
			strings.Contains(c.Image, "sqlserver") || strings.Contains(c.Image, "mssql")
		if looksLikeMSSQL && c.SAPassword() == "" {
			errs = append(errs, fmt.Sprintf("container %q is SQL Server and must set environment SA_PASSWORD (orbit has no built-in default)", name))
		}
	}

	// Cycle detection
	if err := detectCycles(cfg); err != nil {
		errs = append(errs, err.Error())
	}

	// Port conflict detection
	if conflicts := detectPortConflicts(cfg); len(conflicts) > 0 {
		errs = append(errs, conflicts...)
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation errors:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func isValidPullPolicy(value string) bool {
	switch value {
	case "", "always", "if_not_present", "never":
		return true
	default:
		return false
	}
}

func detectCycles(cfg *Config) error {
	// Build adjacency list
	adj := make(map[string][]string)
	for name, s := range cfg.Services {
		adj[name] = s.DependsOn
	}
	for name, c := range cfg.Containers {
		adj[name] = c.DependsOn
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int)

	var dfs func(node string, path []string) error
	dfs = func(node string, path []string) error {
		color[node] = gray
		path = append(path, node)
		for _, dep := range adj[node] {
			if color[dep] == gray {
				// Find the cycle portion of the path
				cycleStart := 0
				for i, n := range path {
					if n == dep {
						cycleStart = i
						break
					}
				}
				cycle := make([]string, len(path[cycleStart:])+1)
				copy(cycle, path[cycleStart:])
				cycle[len(cycle)-1] = dep
				return fmt.Errorf("dependency cycle detected: %s", strings.Join(cycle, " → "))
			}
			if color[dep] == white {
				if err := dfs(dep, path); err != nil {
					return err
				}
			}
		}
		color[node] = black
		return nil
	}

	for name := range adj {
		if color[name] == white {
			if err := dfs(name, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func detectPortConflicts(cfg *Config) []string {
	type portOwner struct {
		name  string
		label string
	}
	used := make(map[int]portOwner)
	var conflicts []string

	register := func(name string, ports map[string]PortDef) {
		for label, pd := range ports {
			if prev, exists := used[pd.Host]; exists {
				conflicts = append(conflicts, fmt.Sprintf(
					"port %d conflict: %s.%s vs %s.%s",
					pd.Host, prev.name, prev.label, name, label,
				))
			} else {
				used[pd.Host] = portOwner{name, label}
			}
		}
	}

	for name, c := range cfg.Containers {
		register(name, c.Ports)
		for _, sc := range c.Sidecars {
			register(name+"/"+sc.Name, sc.Ports)
		}
	}
	for name, s := range cfg.Services {
		register(name, s.Ports)
	}

	return conflicts
}
