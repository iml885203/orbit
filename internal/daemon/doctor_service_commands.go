package daemon

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iml885203/orbit/config"
)

func configuredPythonInterpreterChecks(cfg *config.Config) []DoctorCheck {
	if cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Services))
	for name := range cfg.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	var checks []DoctorCheck
	for _, name := range names {
		service := cfg.Services[name]
		if service == nil || service.Type != "python" {
			continue
		}
		interpreter := commandExecutable(service.Command)
		if interpreter == "" || !strings.ContainsAny(interpreter, `/\`) {
			continue
		}
		path := interpreter
		if !filepath.IsAbs(path) {
			path = filepath.Join(service.Path, path)
		}
		check := DoctorCheck{Name: "Python (" + name + ")"}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			check.Status = CheckFail
			check.Message = "configured interpreter not found: " + path
			check.Hint = "Create the project environment or update services." + name + ".command"
		} else {
			check.Status = CheckPass
			check.Message = "configured interpreter found at " + path
		}
		checks = append(checks, check)
	}
	return checks
}
