package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/iml885203/orbit/config"
)

var numericVersionPattern = regexp.MustCompile(`\d+(?:\.\d+){0,3}`)

func projectRuntimeVersionRequirements(cfg *config.Config) map[string][]HostVersionRequirement {
	requirements := map[string][]HostVersionRequirement{}
	if cfg == nil {
		return requirements
	}
	names := make([]string, 0, len(cfg.Services))
	for name := range cfg.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		service := cfg.Services[name]
		if service == nil || service.Path == "" {
			continue
		}
		switch service.Type {
		case "go":
			if commandBinary(service.Command) == "go" {
				if requirement, ok := goVersionRequirement(name, service.Path); ok {
					requirements["go"] = append(requirements["go"], requirement)
				}
			}
		case "node":
			if commandBinary(service.Command) == "bun" {
				requirements["bun"] = append(requirements["bun"], bunVersionRequirements(name, service.Path)...)
			} else {
				requirements["node"] = append(requirements["node"], nodeVersionRequirements(name, service.Path)...)
			}
		case "python":
			binary := commandBinary(service.Command)
			if binary == "" {
				binary = "python3"
			}
			if binary == "python" || binary == "python3" {
				requirements[binary] = append(requirements[binary], pythonVersionRequirements(name, service.Path)...)
			}
		case "dotnet":
			if requirement, ok := dotnetVersionRequirement(name, service.Path); ok {
				requirements["dotnet"] = append(requirements["dotnet"], requirement)
			}
		}
	}
	return requirements
}

func goVersionRequirement(service, projectPath string) (HostVersionRequirement, bool) {
	source := filepath.Join(projectPath, "go.mod")
	content, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		return HostVersionRequirement{}, false
	}
	requirement := HostVersionRequirement{Service: service, ProjectPath: projectPath, Source: source}
	if err != nil {
		requirement.ParseError = err.Error()
		return requirement, true
	}
	var languageVersion string
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "go":
			languageVersion = fields[1]
		case "toolchain":
			requirement.Requested = strings.TrimPrefix(fields[1], "go")
		}
	}
	if requirement.Requested == "" {
		requirement.Requested = languageVersion
	}
	if requirement.Requested == "" {
		requirement.ParseError = "go version is missing"
	}
	return requirement, true
}

func nodeVersionRequirements(service, projectPath string) []HostVersionRequirement {
	var requirements []HostVersionRequirement
	for _, name := range []string{".nvmrc", ".node-version"} {
		if requirement, ok := plainVersionRequirement(service, projectPath, name); ok {
			requirements = append(requirements, requirement)
		}
	}
	if requirement, ok := toolVersionsRequirement(service, projectPath, "nodejs"); ok {
		requirements = append(requirements, requirement)
	}
	return requirements
}

func pythonVersionRequirements(service, projectPath string) []HostVersionRequirement {
	var requirements []HostVersionRequirement
	if requirement, ok := plainVersionRequirement(service, projectPath, ".python-version"); ok {
		requirements = append(requirements, requirement)
	}
	if requirement, ok := toolVersionsRequirement(service, projectPath, "python"); ok {
		requirements = append(requirements, requirement)
	}
	return requirements
}

func bunVersionRequirements(service, projectPath string) []HostVersionRequirement {
	var requirements []HostVersionRequirement
	if requirement, ok := plainVersionRequirement(service, projectPath, ".bun-version"); ok {
		requirements = append(requirements, requirement)
	}
	if requirement, ok := toolVersionsRequirement(service, projectPath, "bun"); ok {
		requirements = append(requirements, requirement)
	}
	return requirements
}

func plainVersionRequirement(service, projectPath, name string) (HostVersionRequirement, bool) {
	source := filepath.Join(projectPath, name)
	content, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		return HostVersionRequirement{}, false
	}
	requirement := HostVersionRequirement{Service: service, ProjectPath: projectPath, Source: source}
	if err != nil {
		requirement.ParseError = err.Error()
		return requirement, true
	}
	fields := strings.Fields(string(content))
	if len(fields) == 0 {
		requirement.ParseError = "file is empty"
		return requirement, true
	}
	requirement.Requested = fields[0]
	return requirement, true
}

func toolVersionsRequirement(service, projectPath, runtime string) (HostVersionRequirement, bool) {
	source := filepath.Join(projectPath, ".tool-versions")
	content, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		return HostVersionRequirement{}, false
	}
	requirement := HostVersionRequirement{Service: service, ProjectPath: projectPath, Source: source}
	if err != nil {
		requirement.ParseError = err.Error()
		return requirement, true
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == runtime {
			requirement.Requested = fields[1]
			return requirement, true
		}
	}
	return HostVersionRequirement{}, false
}

func dotnetVersionRequirement(service, projectPath string) (HostVersionRequirement, bool) {
	dir := projectPath
	if info, err := os.Stat(projectPath); err == nil && !info.IsDir() {
		dir = filepath.Dir(projectPath)
	}
	source := filepath.Join(dir, "global.json")
	content, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		return HostVersionRequirement{}, false
	}
	requirement := HostVersionRequirement{Service: service, ProjectPath: projectPath, Source: source}
	if err != nil {
		requirement.ParseError = err.Error()
		return requirement, true
	}
	var manifest struct {
		SDK struct {
			Version string `json:"version"`
		} `json:"sdk"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		requirement.ParseError = err.Error()
		return requirement, true
	}
	if strings.TrimSpace(manifest.SDK.Version) == "" {
		requirement.ParseError = "sdk.version is missing"
		return requirement, true
	}
	requirement.Requested = strings.TrimSpace(manifest.SDK.Version)
	return requirement, true
}

func evaluateRuntimeRequirements(tool HostToolCheck, binaryPath, installed, baseMessage string) DoctorCheck {
	if len(tool.Requirements) == 0 {
		return DoctorCheck{Name: tool.Name, Status: CheckPass, Message: baseMessage}
	}
	var matched, failed, unknown []string
	for _, requirement := range tool.Requirements {
		label := requirementLabel(requirement)
		if requirement.ParseError != "" {
			failed = append(failed, label+" cannot be read: "+requirement.ParseError)
			continue
		}
		if tool.Binary == "dotnet" {
			resolved, err := dotnetVersionForProject(binaryPath, requirement.ProjectPath)
			if err != nil {
				failed = append(failed, label+" cannot resolve: "+err.Error())
			} else {
				matched = append(matched, label+" resolves to "+resolved)
			}
			continue
		}
		compatible, comparable := compatibleVersion(installed, requirement.Requested)
		if tool.Binary == "go" {
			compatible, comparable = minimumVersion(installed, requirement.Requested)
		}
		if !comparable {
			unknown = append(unknown, label)
		} else if !compatible {
			failed = append(failed, label+"; installed "+displayVersion(installed))
		} else {
			matched = append(matched, label)
		}
	}
	if len(failed) > 0 {
		message := baseMessage + "; version mismatch: " + strings.Join(failed, "; ")
		if len(matched) > 0 {
			message += "; also " + strings.Join(matched, "; ")
		}
		return DoctorCheck{
			Name:    tool.Name,
			Status:  CheckWarn,
			Message: message,
			Hint:    runtimeVersionHint(tool),
		}
	}
	if len(unknown) > 0 {
		return DoctorCheck{
			Name:    tool.Name,
			Status:  CheckWarn,
			Message: baseMessage + "; cannot verify version alias: " + strings.Join(unknown, "; "),
			Hint:    "Use the project's runtime version manager before `orbit up`",
		}
	}
	return DoctorCheck{
		Name:    tool.Name,
		Status:  CheckPass,
		Message: baseMessage + "; matches " + strings.Join(matched, "; "),
	}
}

func dotnetVersionForProject(binaryPath, projectPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), hostToolVersionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, "--version")
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if version == "" {
		return "", fmt.Errorf("dotnet --version returned no version")
	}
	return version, nil
}

func compatibleVersion(installed, requested string) (bool, bool) {
	installedParts, ok := numericVersionParts(installed)
	if !ok {
		return false, false
	}
	requestedParts, ok := numericVersionParts(requested)
	if !ok {
		return false, false
	}
	if len(requestedParts) > len(installedParts) {
		return false, true
	}
	for i := range requestedParts {
		if installedParts[i] != requestedParts[i] {
			return false, true
		}
	}
	return true, true
}

func minimumVersion(installed, requested string) (bool, bool) {
	installedParts, ok := numericVersionParts(installed)
	if !ok {
		return false, false
	}
	requestedParts, ok := numericVersionParts(requested)
	if !ok {
		return false, false
	}
	width := len(installedParts)
	if len(requestedParts) > width {
		width = len(requestedParts)
	}
	for i := 0; i < width; i++ {
		var installedPart, requestedPart int
		if i < len(installedParts) {
			installedPart = installedParts[i]
		}
		if i < len(requestedParts) {
			requestedPart = requestedParts[i]
		}
		if installedPart > requestedPart {
			return true, true
		}
		if installedPart < requestedPart {
			return false, true
		}
	}
	return true, true
}

func numericVersionParts(value string) ([]int, bool) {
	match := numericVersionPattern.FindString(value)
	if match == "" {
		return nil, false
	}
	fields := strings.Split(match, ".")
	parts := make([]int, len(fields))
	for i, field := range fields {
		part, err := strconv.Atoi(field)
		if err != nil {
			return nil, false
		}
		parts[i] = part
	}
	return parts, true
}

func displayVersion(value string) string {
	if match := numericVersionPattern.FindString(value); match != "" {
		return match
	}
	return value
}

func requirementLabel(requirement HostVersionRequirement) string {
	source := filepath.Base(requirement.Source)
	if requirement.Requested == "" {
		return requirement.Service + " (" + source + ")"
	}
	return requirement.Service + " requires " + requirement.Requested + " (" + source + ")"
}

func runtimeVersionHint(tool HostToolCheck) string {
	if hasConflictingRequirements(tool.Requirements) {
		return fmt.Sprintf(
			"Align the conflicting %s version files in %s if the project does not run correctly",
			tool.Name,
			projectDirectories(tool.Requirements),
		)
	}
	return fmt.Sprintf(
		"Select the project version of %s in %s if the project does not run correctly",
		tool.Name,
		projectDirectories(tool.Requirements),
	)
}

func hasConflictingRequirements(requirements []HostVersionRequirement) bool {
	versions := map[string]bool{}
	for _, requirement := range requirements {
		if requirement.ParseError == "" && requirement.Requested != "" {
			versions[requirement.Requested] = true
		}
	}
	return len(versions) > 1
}

func projectDirectories(requirements []HostVersionRequirement) string {
	seen := map[string]bool{}
	var paths []string
	for _, requirement := range requirements {
		if requirement.ProjectPath == "" || seen[requirement.ProjectPath] {
			continue
		}
		seen[requirement.ProjectPath] = true
		paths = append(paths, requirement.ProjectPath)
	}
	return strings.Join(paths, ", ")
}
