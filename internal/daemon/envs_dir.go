package daemon

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultEnvsDir returns ~/.orbit/envs — the local destination synced env
// configs live in. Sibling of DefaultStatePath / DefaultSettingsPath / etc.
func DefaultEnvsDir() string {
	return filepath.Join(OrbitDir(), "envs")
}

// ListEnvYamls returns the top-level *.yaml filenames in dir, sorted by the
// filesystem's native order (ReadDir). Subdirectories and non-yaml files
// are ignored. Returns nil on any error (caller decides whether to treat
// a missing dir as fatal).
func ListEnvYamls(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		out = append(out, e.Name())
	}
	return out
}
