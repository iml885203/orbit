package cli

import "fmt"

type unsupportedDestructiveJSONCommandError struct {
	command string
	reason  string
}

func NewUnsupportedDestructiveJSONCommandError(command, reason string) error {
	return unsupportedDestructiveJSONCommandError{command: command, reason: reason}
}

func (e unsupportedDestructiveJSONCommandError) Error() string {
	return fmt.Sprintf("%s is destructive and is not supported in --json mode", e.command)
}

func (e unsupportedDestructiveJSONCommandError) CLIJSONActions() []JSONAction {
	return []JSONAction{{
		Command:     e.command,
		Reason:      e.reason,
		Destructive: true,
	}}
}
