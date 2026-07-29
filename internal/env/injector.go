package env

import (
	"fmt"
	"sort"
	"strings"

	"github.com/iml885203/orbit/config"
)

// BuildEnv assembles environment variables for a service, including
// connection strings derived from container configs.
// toggleStates maps "service/VAR_NAME" to on/off. Nil means use config defaults.
func BuildEnv(svc *config.Service, containers map[string]*config.Container, toggleStates map[string]bool) map[string]string {
	env := make(map[string]string)

	// Copy explicit env vars from service config, respecting toggles
	for k, v := range svc.Env {
		if toggle, hasToggle := svc.EnvToggles[k]; hasToggle {
			key := svc.Name + "/" + k
			if on, ok := toggleStates[key]; ok {
				if !on {
					continue // toggle off — skip
				}
			} else if !toggle.Default {
				continue // no persisted state, default is off — skip
			}
		}
		env[k] = v
	}

	// Auto-inject connection strings for dependencies
	for _, dep := range svc.DependsOn {
		c, ok := containers[dep]
		if !ok {
			continue
		}
		connStrings := buildConnectionStrings(dep, c)
		for k, v := range connStrings {
			// Don't override explicit env vars
			if _, exists := env[k]; !exists {
				env[k] = v
			}
		}
	}

	return env
}

// InjectServicePorts exposes runtime-selected ports to host services that
// explicitly opted into automatic port recovery. An explicit value unrelated
// to the preferred port wins; a value derived from that preference follows the
// runtime selection.
func InjectServicePorts(target map[string]string, ports map[string]config.PortDef) {
	autoPorts := make(map[string]config.PortDef)
	for label, definition := range ports {
		if definition.IsAuto() {
			autoPorts[label] = definition
		}
	}
	for label, definition := range autoPorts {
		key := strings.ToUpper(strings.ReplaceAll(label, "-", "_")) + "_PORT"
		value, exists := target[key]
		if !exists || value == fmt.Sprintf("%d", definition.PreferredHost()) {
			target[key] = fmt.Sprintf("%d", definition.Host)
		}
	}
	if len(ports) == 1 && len(autoPorts) == 1 {
		for _, definition := range autoPorts {
			value, exists := target["PORT"]
			if !exists || value == fmt.Sprintf("%d", definition.PreferredHost()) {
				target["PORT"] = fmt.Sprintf("%d", definition.Host)
			}
		}
	}
}

func buildConnectionStrings(name string, c *config.Container) map[string]string {
	env := make(map[string]string)
	upper := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	connKey := "ConnectionStrings__" + name

	image := strings.ToLower(c.Image)

	switch {
	case strings.Contains(image, "redis"):
		port := getPort(c.Ports, "redis", 6379)
		env["REDIS_URL"] = fmt.Sprintf("localhost:%d", port)
		env[connKey] = fmt.Sprintf("localhost:%d", port)
		env[upper+"_HOST"] = "localhost"
		env[upper+"_PORT"] = fmt.Sprintf("%d", port)

	case strings.Contains(image, "kafka"):
		port := getPort(c.Ports, "broker", 9092)
		env["KAFKA_BOOTSTRAP_SERVERS"] = fmt.Sprintf("localhost:%d", port)
		env[connKey] = fmt.Sprintf("localhost:%d", port)
		env[upper+"_HOST"] = "localhost"
		env[upper+"_PORT"] = fmt.Sprintf("%d", port)

	case strings.Contains(image, "mongo"):
		port := getPort(c.Ports, "mongo", 27017)
		connStr := fmt.Sprintf("mongodb://localhost:%d", port)
		env["MONGODB_URL"] = connStr
		env[connKey] = connStr
		env[upper+"_HOST"] = "localhost"
		env[upper+"_PORT"] = fmt.Sprintf("%d", port)

	case strings.Contains(image, "postgres"):
		port := getPort(c.Ports, "postgres", 5432)
		env["DATABASE_URL"] = fmt.Sprintf("postgres://localhost:%d", port)
		env[connKey] = fmt.Sprintf("Host=localhost;Port=%d;", port)
		env[upper+"_HOST"] = "localhost"
		env[upper+"_PORT"] = fmt.Sprintf("%d", port)

	default:
		for label, pd := range c.Ports {
			env[upper+"_"+strings.ToUpper(label)+"_PORT"] = fmt.Sprintf("%d", pd.Host)
		}
		env[upper+"_HOST"] = "localhost"
	}

	return env
}

func getPort(ports map[string]config.PortDef, preferred string, fallback int) int {
	if pd, ok := ports[preferred]; ok {
		return pd.Host
	}
	for _, pd := range ports {
		return pd.Host
	}
	return fallback
}

// EnvVarsForDependency returns the env var names that would be injected for
// a service depending on container c (no values, no service context).
// Returns empty for nil containers or service-to-service dependencies (the
// injector currently only auto-injects for container dependencies).
func EnvVarsForDependency(depName string, c *config.Container) []string {
	if c == nil {
		return nil
	}
	connStrings := buildConnectionStrings(depName, c)
	keys := make([]string, 0, len(connStrings))
	for k := range connStrings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// EnvEntry is one resolved env var with source attribution for a service.
type EnvEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Source is "explicit", "toggle", or "dependency".
	Source string `json:"source"`
	// Dependency is the container name this var came from when Source == "dependency".
	// Empty otherwise.
	Dependency string `json:"dependency,omitempty"`
}

// AnnotatedEnv assembles the full env for a service (same logic as BuildEnv)
// and annotates each key with its source. It is the single source of truth for
// "which dep injected this key" — both /api/service-env and any future caller
// should use this rather than re-implementing the attribution logic.
//
// Priority: explicit env wins over dependency injection, matching BuildEnv.
// If two deps would inject the same key the first dep listed wins.
func AnnotatedEnv(svc *config.Service, containers map[string]*config.Container, toggleStates map[string]bool) []EnvEntry {
	// Build the resolved env map (handles toggle filtering and dep injection).
	envMap := BuildEnv(svc, containers, toggleStates)

	// Build a key→dep attribution map for dependency-injected keys.
	// First dep listed for a key wins, matching BuildEnv's "don't override" policy.
	keyToDep := make(map[string]string, len(svc.DependsOn)*4)
	for _, dep := range svc.DependsOn {
		c, ok := containers[dep]
		if !ok {
			continue
		}
		for _, k := range EnvVarsForDependency(dep, c) {
			if _, claimed := keyToDep[k]; !claimed {
				keyToDep[k] = dep
			}
		}
	}

	entries := make([]EnvEntry, 0, len(envMap))
	for k, v := range envMap {
		e := EnvEntry{Key: k, Value: v, Source: "explicit"}
		if _, isToggle := svc.EnvToggles[k]; isToggle {
			e.Source = "toggle"
		} else if dep, ok := keyToDep[k]; ok {
			e.Source = "dependency"
			e.Dependency = dep
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries
}
