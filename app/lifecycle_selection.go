package app

import (
	"sort"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/suggest"
)

type resourceNameError struct {
	requested       string
	available       []string
	suggestion      string
	correctedHuman  string
	correctedAction string
}

func (e resourceNameError) Error() string {
	message := "unknown resource: " + e.requested
	if e.suggestion != "" {
		return message + " (did you mean " + e.suggestion + "?)"
	}
	if len(e.available) == 0 {
		return message + " (this environment defines no resources)"
	}
	return message + " (available: " + strings.Join(e.available, ", ") + ")"
}

func (e resourceNameError) ErrorCode() string {
	return "unknown_resource"
}

func (e resourceNameError) CLIJSONHint() string {
	if e.suggestion != "" {
		return "Retry with the closest configured resource name."
	}
	return "Run 'orbit status --json' to inspect configured resources."
}

func (e resourceNameError) CLIJSONReplacementActions() []cli.JSONAction {
	if e.correctedAction != "" {
		return []cli.JSONAction{{
			Command:     e.correctedAction,
			Reason:      "Retry with the configured resource " + e.suggestion + ".",
			Destructive: false,
		}}
	}
	return []cli.JSONAction{cli.StatusAction()}
}

func (e resourceNameError) CLIHumanNextCommand() string {
	if e.correctedHuman != "" {
		return e.correctedHuman
	}
	return "orbit status"
}

type groupNameError struct {
	requested       string
	available       []string
	suggestion      string
	correctedHuman  string
	correctedAction string
}

func (e groupNameError) Error() string {
	message := "unknown group: " + e.requested
	if e.suggestion != "" {
		return message + " (did you mean " + e.suggestion + "?)"
	}
	if len(e.available) == 0 {
		return message + " (this environment defines no groups)"
	}
	return message + " (available: " + strings.Join(e.available, ", ") + ")"
}

func (e groupNameError) ErrorCode() string {
	return "unknown_group"
}

func (e groupNameError) CLIJSONHint() string {
	return "Use a group name defined by the active environment."
}

func (e groupNameError) CLIJSONReplacementActions() []cli.JSONAction {
	if e.correctedAction == "" {
		return nil
	}
	return []cli.JSONAction{{
		Command:     e.correctedAction,
		Reason:      "Retry with the configured group " + e.suggestion + ".",
		Destructive: false,
	}}
}

func (e groupNameError) CLIHumanNextCommand() string {
	return e.correctedHuman
}

func newResourceNameError(
	status *daemon.StatusResponse,
	requested string,
	correctedCommand func(string) string,
) error {
	return newResourceNameErrorFromNames(lifecycleResourceNames(status), requested, correctedCommand)
}

func newResourceNameErrorFromNames(
	available []string,
	requested string,
	correctedCommand func(string) string,
) error {
	suggestion := closestResourceName(requested, available)
	human := ""
	action := ""
	if suggestion != "" {
		human = correctedCommand(suggestion)
		action = human + " --json"
	}
	return resourceNameError{
		requested:       requested,
		available:       available,
		suggestion:      suggestion,
		correctedHuman:  human,
		correctedAction: action,
	}
}

func lifecycleResourceNames(status *daemon.StatusResponse) []string {
	if status == nil {
		return nil
	}
	names := make([]string, 0, len(status.Resources))
	for _, resource := range status.Resources {
		names = append(names, resource.Name)
	}
	sort.Strings(names)
	return names
}

func closestResourceName(requested string, available []string) string {
	best := ""
	bestDistance := len([]rune(requested)) + 1
	tied := false
	for _, candidate := range available {
		distance := suggest.Distance(strings.ToLower(requested), strings.ToLower(candidate))
		switch {
		case distance < bestDistance:
			best = candidate
			bestDistance = distance
			tied = false
		case distance == bestDistance:
			tied = true
		}
	}
	limit := 2
	if len([]rune(requested)) <= 3 {
		limit = 1
	}
	if tied || bestDistance > limit {
		return ""
	}
	return best
}

func validateLifecycleSelection(resources, selectedGroups []string, selectedInfra bool) error {
	switch {
	case selectedInfra && len(resources) > 0:
		return cli.NewInvalidArgumentError("resource names and --infra cannot be used together")
	case selectedInfra && len(selectedGroups) > 0:
		return cli.NewInvalidArgumentError("--group and --infra cannot be used together")
	case len(resources) > 0 && len(selectedGroups) > 0:
		return cli.NewInvalidArgumentError("resource names and --group cannot be used together")
	default:
		return nil
	}
}

func validateConfiguredLifecycleSelection(path string, resources, selectedGroups []string, command string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return nil
	}
	if err := validateConfiguredResourceNames(cfg, resources, func(index int, suggestion string) string {
		corrected := append([]string{}, resources...)
		corrected[index] = suggestion
		return "orbit " + command + " " + strings.Join(corrected, " ")
	}); err != nil {
		return err
	}
	availableGroups := sortedKeys(cfg.Groups)
	for index, name := range selectedGroups {
		if sortedNameExists(availableGroups, name) {
			continue
		}
		suggestion := closestResourceName(name, availableGroups)
		human := ""
		action := ""
		if suggestion != "" {
			corrected := append([]string{}, selectedGroups...)
			corrected[index] = suggestion
			human = "orbit " + command + " --group " + strings.Join(corrected, ",")
			action = human + " --json"
		}
		return groupNameError{
			requested:       name,
			available:       availableGroups,
			suggestion:      suggestion,
			correctedHuman:  human,
			correctedAction: action,
		}
	}
	return nil
}

func validateConfiguredCommandResources(
	path string,
	resources []string,
	correctedCommand func(index int, suggestion string) string,
) error {
	cfg, err := config.Load(path)
	if err != nil {
		return nil
	}
	return validateConfiguredResourceNames(cfg, resources, correctedCommand)
}

func validateConfiguredResourceNames(
	cfg *config.Config,
	resources []string,
	correctedCommand func(index int, suggestion string) string,
) error {
	available := configuredLifecycleResourceNames(cfg)
	for index, name := range resources {
		if sortedNameExists(available, name) {
			continue
		}
		return newResourceNameErrorFromNames(available, name, func(suggestion string) string {
			return correctedCommand(index, suggestion)
		})
	}
	return nil
}

func configuredLifecycleResourceNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Containers)+len(cfg.Services))
	for name := range cfg.Containers {
		names = append(names, name)
	}
	for name := range cfg.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedNameExists(names []string, requested string) bool {
	index := sort.SearchStrings(names, requested)
	return index < len(names) && names[index] == requested
}
