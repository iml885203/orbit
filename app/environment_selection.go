package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/iml885203/orbit/cli"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/shellquote"
)

const (
	environmentSelectionNone        = "none"
	environmentSelectionSelected    = "selected"
	environmentSelectionUnavailable = "unavailable"
)

type environmentChoice struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Selected bool   `json:"selected"`
}

type environmentSelection struct {
	State                 string              `json:"state"`
	Source                string              `json:"source,omitempty"`
	SelectedName          string              `json:"selected_name,omitempty"`
	SelectedPath          string              `json:"selected_path,omitempty"`
	ContextSwitchRequired bool                `json:"context_switch_required,omitempty"`
	RunningName           string              `json:"running_name,omitempty"`
	RunningPath           string              `json:"running_path,omitempty"`
	Environments          []environmentChoice `json:"environments"`
}

type environmentSelectionRequiredError struct {
	message string
	hint    string
}

func newEnvironmentSelectionRequiredError(selection environmentSelection) error {
	next := "orbit env sync"
	if len(selection.Environments) == 1 {
		next = environmentSwitchCommand(selection.Environments[0].Name, false)
	} else if len(selection.Environments) > 1 {
		next = "orbit env list"
	}
	message := "the selected environment is no longer available — run '" + next + "'"
	return cli.WithJSONActions(
		environmentSelectionRequiredError{
			message: message,
			hint:    environmentSelectionHint(selection),
		},
		environmentSelectionActions(selection),
	)
}

func (e environmentSelectionRequiredError) Error() string {
	return e.message
}

func (e environmentSelectionRequiredError) ErrorCode() string {
	return "environment_selection_required"
}

func (e environmentSelectionRequiredError) CLIJSONHint() string {
	return e.hint
}

func environmentSelectionMessage(selection environmentSelection) string {
	if len(selection.Environments) == 0 {
		return fmt.Sprintf("Environment %q is no longer available. Sync environments to restore it.", selection.SelectedName)
	}
	return fmt.Sprintf("Environment %q is no longer available. Select an available environment.", selection.SelectedName)
}

func environmentSelectionHint(selection environmentSelection) string {
	if len(selection.Environments) == 0 {
		return "Sync environments before selecting one."
	}
	return "Select one of the available environments reported by Orbit."
}

func readEnvironmentSelection() environmentSelection {
	selectedPath := readCurrentEnv()
	selection := environmentSelection{
		State:        environmentSelectionNone,
		SelectedName: daemonsrv.EnvShortName(selectedPath),
		SelectedPath: selectedPath,
		Environments: []environmentChoice{},
	}

	for _, filename := range daemonsrv.ListEnvYamls(envsDestDir()) {
		path := filepath.Join(envsDestDir(), filename)
		selection.Environments = append(selection.Environments, environmentChoice{
			Name:     daemonsrv.EnvShortName(filename),
			Path:     path,
			Selected: sameFilePath(path, selectedPath),
		})
	}

	if selectedPath == "" {
		return selection
	}
	info, err := os.Stat(selectedPath)
	if err != nil || info.IsDir() {
		selection.State = environmentSelectionUnavailable
		return selection
	}
	selection.State = environmentSelectionSelected
	return selection
}

func sameFilePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func activeEnvironmentSelection(selection environmentSelection, configPath string) environmentSelection {
	if usesDiscoveredProjectConfig(configPath) {
		selection.State = environmentSelectionSelected
		selection.Source = "project"
		selection.SelectedName = projectContextName(configPath)
		selection.SelectedPath = configPath
		for i := range selection.Environments {
			selection.Environments[i].Selected = false
		}
		return selection
	}
	if selection.State == environmentSelectionSelected {
		selection.Source = "managed"
	}
	return selection
}

func environmentSelectionBlocksConfig(selection environmentSelection, configPath string) bool {
	return selection.State == environmentSelectionUnavailable &&
		sameFilePath(selection.SelectedPath, configPath)
}

func environmentSelectionActions(selection environmentSelection) []cli.JSONAction {
	if selection.State == environmentSelectionSelected {
		return nil
	}
	if len(selection.Environments) == 0 {
		return []cli.JSONAction{{
			Command: "orbit env sync --json",
			Reason:  "Fetch available environments before selecting one.",
		}}
	}

	actions := make([]cli.JSONAction, 0, len(selection.Environments))
	for _, environment := range selection.Environments {
		actions = append(actions, cli.JSONAction{
			Command: environmentSwitchCommand(environment.Name, true),
			Reason:  "Select the " + environment.Name + " environment.",
		})
	}
	return actions
}

func environmentSwitchCommand(name string, jsonOutput bool) string {
	command := "orbit switch " + shellquote.Quote(name)
	if jsonOutput {
		command += " --json"
	}
	return command
}
