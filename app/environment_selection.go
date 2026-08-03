package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsource"
	"github.com/iml885203/orbit/internal/envsync"
	"github.com/iml885203/orbit/internal/shellquote"
)

const (
	environmentSelectionNone        = "none"
	environmentSelectionSelected    = "selected"
	environmentSelectionUnavailable = "unavailable"
)

type environmentChoice struct {
	Identity string `json:"identity,omitempty"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Selected bool   `json:"selected"`
	Running  bool   `json:"running,omitempty"`
}

type environmentSourceChoice struct {
	Name          string              `json:"name"`
	Type          string              `json:"type"`
	Location      string              `json:"location"`
	Workspace     string              `json:"workspace,omitempty"`
	Default       bool                `json:"default"`
	Ref           string              `json:"ref,omitempty"`
	ResolvedRef   string              `json:"resolved_ref,omitempty"`
	Commit        string              `json:"commit,omitempty"`
	LastSyncError string              `json:"last_sync_error,omitempty"`
	LastSyncAt    time.Time           `json:"last_sync_at,omitempty"`
	Environments  []environmentChoice `json:"environments"`
}

type environmentSelection struct {
	State                 string                    `json:"state"`
	Source                string                    `json:"source,omitempty"`
	SelectedName          string                    `json:"selected_name,omitempty"`
	SelectedIdentity      string                    `json:"selected_identity,omitempty"`
	SelectedPath          string                    `json:"selected_path,omitempty"`
	ContextSwitchRequired bool                      `json:"context_switch_required,omitempty"`
	RunningName           string                    `json:"running_name,omitempty"`
	RunningPath           string                    `json:"running_path,omitempty"`
	ManagedSource         *envsync.RepositorySource `json:"managed_source,omitempty"`
	ManagedSelection      *environmentChoice        `json:"managed_selection,omitempty"`
	Sources               []environmentSourceChoice `json:"sources"`
	Environments          []environmentChoice       `json:"environments"`
}

type environmentSelectionRequiredError struct {
	message string
	hint    string
}

func newEnvironmentSelectionRequiredError(selection environmentSelection) error {
	next := "orbit source sync"
	if len(selection.Environments) == 1 {
		next = environmentSwitchCommand(environmentChoiceSwitchTarget(selection.Environments[0]), false)
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
		Sources:      []environmentSourceChoice{},
		Environments: []environmentChoice{},
	}
	registry, err := sourceRegistry()
	if err == nil {
		if _, identity, found := registry.SourceForPath(daemon.OrbitDir(), selectedPath); found {
			selection.SelectedIdentity = identity
		}
		for _, source := range registry.List() {
			sourceChoice := environmentSourceChoice{
				Name: source.Name, Type: source.Type, Location: source.Location(), Workspace: source.Workspace,
				Default: source.Default, Ref: source.Ref, ResolvedRef: source.ResolvedRef, Commit: source.Commit,
				LastSyncError: source.LastSyncError, LastSyncAt: source.LastSyncAt, Environments: []environmentChoice{},
			}
			for _, filename := range daemonsrv.ListEnvYamls(envsource.EnvsDir(daemon.OrbitDir(), source.Name)) {
				path := filepath.Join(envsource.EnvsDir(daemon.OrbitDir(), source.Name), filename)
				choice := environmentChoice{
					Identity: envsource.Identity(source.Name, filename),
					Name:     daemonsrv.EnvShortName(filename), Path: path, Selected: sameFilePath(path, selectedPath),
				}
				if choice.Selected {
					selection.SelectedIdentity = choice.Identity
					selection.SelectedName = choice.Name
				}
				sourceChoice.Environments = append(sourceChoice.Environments, choice)
				selection.Environments = append(selection.Environments, choice)
			}
			selection.Sources = append(selection.Sources, sourceChoice)
		}
	}
	if len(selection.Sources) == 0 {
		if source, err := envsync.ReadRepositorySource(envsDestDir()); err == nil && source.Commit != "" {
			selection.ManagedSource = &source
		}
		for _, filename := range daemonsrv.ListEnvYamls(envsDestDir()) {
			path := filepath.Join(envsDestDir(), filename)
			selection.Environments = append(selection.Environments, environmentChoice{Name: daemonsrv.EnvShortName(filename), Path: path, Selected: sameFilePath(path, selectedPath)})
		}
	}
	sort.Slice(selection.Environments, func(i, j int) bool { return selection.Environments[i].Identity < selection.Environments[j].Identity })

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
	if leftErr != nil || rightErr != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(leftAbs); err == nil {
		leftAbs = resolved
	}
	if resolved, err := filepath.EvalSymlinks(rightAbs); err == nil {
		rightAbs = resolved
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func activeEnvironmentSelection(selection environmentSelection, configPath string) environmentSelection {
	return activeEnvironmentSelectionWithKind(selection, configPath, environmentContextKind(configPath))
}

func activeEnvironmentSelectionWithKind(selection environmentSelection, configPath, contextKind string) environmentSelection {
	if contextKind == "" {
		contextKind = environmentContextKind(configPath)
	}
	if contextKind == "project" || contextKind == "explicit" {
		if selection.SelectedPath != "" {
			selection.ManagedSelection = &environmentChoice{
				Name: selection.SelectedName,
				Path: selection.SelectedPath,
			}
		}
		selection.State = environmentSelectionSelected
		selection.Source = contextKind
		selection.SelectedName = projectContextName(configPath)
		selection.SelectedPath = configPath
		selection.ManagedSource = nil
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
			Command: "orbit source sync --json",
			Reason:  "Fetch available environments before selecting one.",
		}}
	}

	actions := make([]cli.JSONAction, 0, len(selection.Environments))
	for _, environment := range selection.Environments {
		target := environmentChoiceSwitchTarget(environment)
		actions = append(actions, cli.JSONAction{
			Command: environmentSwitchCommand(target, true),
			Reason:  "Select the " + target + " environment.",
		})
	}
	return actions
}

func environmentChoiceSwitchTarget(environment environmentChoice) string {
	if environment.Identity != "" {
		return environment.Identity
	}
	return environment.Name
}

func environmentSwitchCommand(name string, jsonOutput bool) string {
	command := "orbit switch " + shellquote.Quote(name)
	if jsonOutput {
		command += " --json"
	}
	return command
}
