// Package preflight runs readiness checks shared by `orbit up`, `orbit doctor`,
// and others. Each check returns a structured result so callers can decide
// whether to block, warn, or just display.
package preflight

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Check is the result of a single preflight probe.
type Check struct {
	Name    string
	OK      bool
	Message string
	Fix     string // suggested remediation, e.g. "run `orbit init`"
}

// CheckEnvsReady verifies the user has a populated envs directory and a
// valid active env selection. Returns checks in stable order.
func CheckEnvsReady(envsDir, activeEnvPath string) []Check {
	var out []Check

	// 1. envsDir exists.
	info, err := os.Stat(envsDir)
	switch {
	case os.IsNotExist(err):
		out = append(out, Check{
			Name:    "Envs directory",
			OK:      false,
			Message: envsDir + " does not exist",
			Fix:     "run `orbit init` to set up",
		})
		return out // can't check further
	case err != nil:
		out = append(out, Check{
			Name: "Envs directory", OK: false, Message: err.Error(),
		})
		return out
	case !info.IsDir():
		out = append(out, Check{
			Name: "Envs directory", OK: false, Message: envsDir + " is not a directory",
		})
		return out
	}
	out = append(out, Check{Name: "Envs directory", OK: true, Message: envsDir})

	// 2. Contains at least one top-level *.yaml.
	yamls, err := listTopLevelYamls(envsDir)
	if err != nil {
		out = append(out, Check{Name: "Env configs", OK: false, Message: err.Error()})
		return out
	}
	if len(yamls) == 0 {
		out = append(out, Check{
			Name:    "Env configs",
			OK:      false,
			Message: "no *.yaml files in " + envsDir,
			Fix:     "run `orbit env sync` to pull configs",
		})
		return out
	}
	out = append(out, Check{
		Name: "Env configs", OK: true,
		Message: fmt.Sprintf("%d config(s): %s", len(yamls), strings.Join(yamls, ", ")),
	})

	// 3. Active env, if provided, points to an existing file.
	if activeEnvPath != "" {
		if _, err := os.Stat(activeEnvPath); err != nil {
			out = append(out, Check{
				Name:    "Active env",
				OK:      false,
				Message: activeEnvPath + " not found",
				Fix:     "run `orbit switch <name>` to select an available env",
			})
		} else {
			out = append(out, Check{
				Name: "Active env", OK: true, Message: filepath.Base(activeEnvPath),
			})
		}
	}

	return out
}

func listTopLevelYamls(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}
