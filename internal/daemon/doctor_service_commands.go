package daemon

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/iml885203/orbit/config"
)

func configuredServiceCommandChecks(cfg *config.Config) []DoctorCheck {
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
		if service == nil {
			continue
		}
		if check, ok := configuredCommandCheck(
			"Command ("+name+")",
			service.Command,
			service.Path,
			"services."+name+".command",
		); ok {
			checks = append(checks, check)
		}
		for i, command := range service.PreStart {
			if check, ok := configuredCommandCheck(
				"Pre-start ("+name+" #"+strconv.Itoa(i+1)+")",
				command,
				service.Path,
				"services."+name+".pre_start entry "+strconv.Itoa(i+1),
			); ok {
				checks = append(checks, check)
			}
		}
	}
	return checks
}

func configuredCommandCheck(name, command, workingDirectory, field string) (DoctorCheck, bool) {
	executable := commandExecutable(command)
	if executable == "" {
		return DoctorCheck{}, false
	}
	if !strings.ContainsAny(executable, `/\`) && isKnownRuntime(executable) {
		return DoctorCheck{}, false
	}
	resolved := executable
	var err error
	pathLike := strings.ContainsAny(executable, `/\`)
	if pathLike {
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(workingDirectory, resolved)
		}
	}
	requestedPath := resolved
	resolved, err = exec.LookPath(resolved)
	check := DoctorCheck{Name: name}
	if err != nil {
		check.Status = CheckFail
		missing := executable
		if pathLike {
			missing = requestedPath
			check.Hint = "Create the executable or update " + field
		} else {
			check.Hint = "Install " + executable + " or update " + field
		}
		check.Message = "executable not found: " + missing
		return check, true
	}
	check.Status = CheckPass
	check.Message = "executable found at " + resolved
	return check, true
}
