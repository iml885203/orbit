package app

import "path/filepath"

type setupRequiredError struct{}

func (setupRequiredError) Error() string {
	return "Orbit is not set up yet."
}

func (setupRequiredError) ErrorCode() string {
	return "setup_required"
}

func (setupRequiredError) CLIJSONHint() string {
	return "Set up Orbit before running environment commands."
}

func (setupRequiredError) CLIHumanNextCommand() string {
	return "orbit init"
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
