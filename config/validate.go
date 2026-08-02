package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Validate checks config for errors: missing refs, cycles, port conflicts.
func Validate(cfg *Config) error {
	var errs []string
	if cfg.Settings.ShutdownTimeout < 0 {
		errs = append(errs, "settings.shutdown_timeout must be positive")
	}
	if cfg.Settings.HealthCheckInterval < 0 {
		errs = append(errs, "settings.health_check_interval must be positive")
	}
	if cfg.Settings.DockerPollInterval < 0 {
		errs = append(errs, "settings.docker_poll_interval must be positive")
	}

	known := make(map[string]bool)
	for name := range cfg.Containers {
		known[name] = true
	}
	for name := range cfg.Services {
		// Same name in both containers + services is allowed (dev/container mode toggle)
		known[name] = true
	}
	knownNames := make([]string, 0, len(known))
	for name := range known {
		knownNames = append(knownNames, name)
	}
	sort.Strings(knownNames)

	// Check dependency references exist
	for name, s := range cfg.Services {
		if s.Kind != "" && !validKinds[s.Kind] {
			errs = append(errs, fmt.Sprintf(
				"service %q has invalid kind %q%s (expected frontend, backend, or infra)",
				name,
				s.Kind,
				schemaValueSuggestion(s.Kind, "frontend", "backend", "infra"),
			))
		}
		if err := validateServiceURL(name, s); err != nil {
			errs = append(errs, err.Error())
		}
		errs = append(errs, validateHealthCheck("service", name, s.HealthCheck, false)...)
		for _, dep := range s.DependsOn {
			if !known[dep] {
				errs = append(errs, fmt.Sprintf(
					"service %q depends on unknown %q%s",
					name,
					dep,
					configuredNameSuggestion(dep, knownNames),
				))
			}
		}
	}
	for name, c := range cfg.Containers {
		if c.Kind != "" && !validKinds[c.Kind] {
			errs = append(errs, fmt.Sprintf(
				"container %q has invalid kind %q%s (expected frontend, backend, or infra)",
				name,
				c.Kind,
				schemaValueSuggestion(c.Kind, "frontend", "backend", "infra"),
			))
		}
		errs = append(errs, validateHealthCheck("container", name, c.HealthCheck, true)...)
		if !isValidPullPolicy(c.PullPolicy) {
			errs = append(errs, fmt.Sprintf(
				"container %q has invalid pull_policy %q%s (expected always, if_not_present, or never)",
				name,
				c.PullPolicy,
				schemaValueSuggestion(c.PullPolicy, "always", "if_not_present", "never"),
			))
		}
		for _, dep := range c.DependsOn {
			if !known[dep] {
				errs = append(errs, fmt.Sprintf(
					"container %q depends on unknown %q%s",
					name,
					dep,
					configuredNameSuggestion(dep, knownNames),
				))
			}
		}
		for _, sc := range c.Sidecars {
			if !isValidPullPolicy(sc.PullPolicy) {
				errs = append(errs, fmt.Sprintf(
					"sidecar %q/%q has invalid pull_policy %q%s (expected always, if_not_present, or never)",
					name,
					sc.Name,
					sc.PullPolicy,
					schemaValueSuggestion(sc.PullPolicy, "always", "if_not_present", "never"),
				))
			}
		}
		if c.Seed != nil {
			if strings.TrimSpace(c.Seed.Command) == "" {
				errs = append(errs, fmt.Sprintf("container %q seed.command is required", name))
			}
			if len(c.Seed.Files) == 0 {
				errs = append(errs, fmt.Sprintf("container %q seed.files must contain at least one file", name))
			}
		}
	}

	// Check group service references
	for gname, g := range cfg.Groups {
		for _, svc := range g.Services {
			if !known[svc] {
				errs = append(errs, fmt.Sprintf(
					"group %q references unknown resource %q%s",
					gname,
					svc,
					configuredNameSuggestion(svc, knownNames),
				))
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

	if err := validateExtensionSections(cfg); err != nil {
		errs = append(errs, err.Error())
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

func configuredNameSuggestion(requested string, available []string) string {
	suggestion := closestName(requested, available)
	if suggestion == "" {
		return ""
	}
	return ` (did you mean "` + suggestion + `"?)`
}

func validateServiceURL(name string, service *Service) error {
	if strings.TrimSpace(service.URL) == "" {
		return nil
	}
	endpoint, err := url.Parse(service.URL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return fmt.Errorf("service %q url must be an absolute http or https URL", name)
	}
	host := endpoint.Hostname()
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return nil
	}
	port, ok := service.Ports[endpoint.Scheme]
	if !ok {
		return nil
	}
	endpointPort := endpoint.Port()
	if endpointPort == "" {
		if endpoint.Scheme == "http" {
			endpointPort = "80"
		} else {
			endpointPort = "443"
		}
	}
	if endpointPort != strconv.Itoa(port.Host) {
		return fmt.Errorf(
			"service %q url uses port %s but ports.%s declares %d",
			name,
			endpointPort,
			endpoint.Scheme,
			port.Host,
		)
	}
	return nil
}

func validateHealthCheck(resourceType, name string, check *HealthCheckConfig, container bool) []string {
	if check == nil {
		return nil
	}
	prefix := fmt.Sprintf("%s %q health_check", resourceType, name)
	var errs []string
	switch check.Type {
	case "http", "tcp":
		if check.Port == 0 {
			errs = append(errs, prefix+".port is required when ports does not identify one endpoint")
		}
	case "log":
		if strings.TrimSpace(check.Pattern) == "" {
			errs = append(errs, prefix+".pattern is required for type log")
		} else if _, err := regexp.Compile(check.Pattern); err != nil {
			errs = append(errs, prefix+".pattern is invalid: "+err.Error())
		}
	case "exec":
		if !container {
			errs = append(errs, prefix+" type exec is only supported for containers")
		} else if len(check.Command) == 0 {
			errs = append(errs, prefix+".command is required for type exec")
		}
	case "healthcheck":
		if !container {
			errs = append(errs, prefix+" type healthcheck is only supported for containers")
		}
	case "":
		errs = append(errs, prefix+".type is required")
	default:
		errs = append(
			errs,
			prefix+" has unknown type "+fmt.Sprintf("%q", check.Type)+
				schemaValueSuggestion(check.Type, "http", "tcp", "log", "exec", "healthcheck"),
		)
	}
	if check.Interval <= 0 {
		errs = append(errs, prefix+".interval must be positive")
	}
	if check.Timeout <= 0 {
		errs = append(errs, prefix+".timeout must be positive")
	}
	if check.Retries < 1 {
		errs = append(errs, prefix+".retries must be at least 1")
	}
	if check.FailureThreshold < 1 {
		errs = append(errs, prefix+".failure_threshold must be at least 1")
	}
	return errs
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
