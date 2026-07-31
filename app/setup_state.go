package app

import (
	"path/filepath"

	"github.com/iml885203/orbit/cli"
)

type setupRequiredError struct{}

func (setupRequiredError) Error() string {
	return "Orbit is not set up yet."
}

func (setupRequiredError) ErrorCode() string {
	return "setup_required"
}

func (setupRequiredError) CLIJSONHint() string {
	return "Set up Orbit before running environment commands. A project directory containing " +
		projectConfigName + " needs no setup."
}

func (setupRequiredError) CLIJSONReplacementActions() []cli.JSONAction {
	return []cli.JSONAction{{
		Command:     "orbit init --yes --json",
		Reason:      "Set up a shared or demo environment without prompting.",
		Destructive: false,
	}}
}

func (setupRequiredError) CLIHumanNextCommand() string {
	return "orbit init"
}

// The project-local path is named as context rather than a second command:
// 'orbit init' stays the one runnable action, but a user inside their own
// project cannot otherwise discover that a project config skips setup entirely.
func (setupRequiredError) CLIHumanContext() string {
	return "Already have " + projectConfigName + " in this project? Run Orbit from that directory instead."
}

func setupRequired(selection environmentSelection, path string) bool {
	if selection.State != environmentSelectionNone || configFileExists(path) {
		return false
	}
	if path == "" {
		return true
	}
	return distribution.DefaultEnv != "" &&
		sameFilePath(path, filepath.Join(envsDestDir(), distribution.DefaultEnv))
}
