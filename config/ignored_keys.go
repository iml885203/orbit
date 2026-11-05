package config

import "strings"

// Top-level keys starting with x- are ignored, as in Docker Compose: a place
// for shared YAML anchors that no section validates or inherits.
func isIgnoredTopLevelKey(name string) bool {
	return strings.HasPrefix(name, "x-")
}

func dropIgnoredTopLevelKeys(cfg *Config) {
	for name := range cfg.Extensions {
		if isIgnoredTopLevelKey(name) {
			delete(cfg.Extensions, name)
		}
	}
}
