package app

import (
	"errors"
	"os"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type unknownFlagGuidanceError struct {
	err             error
	suggestion      string
	nextHuman       string
	replacementJSON string
}

func (e unknownFlagGuidanceError) Error() string {
	if e.suggestion == "" {
		return e.err.Error()
	}
	return e.err.Error() + " (did you mean --" + e.suggestion + "?)"
}

func (e unknownFlagGuidanceError) Unwrap() error {
	return e.err
}

func (e unknownFlagGuidanceError) ErrorCode() string {
	return "invalid_argument"
}

func (e unknownFlagGuidanceError) CLIJSONHint() string {
	if e.suggestion != "" {
		return "Retry with the closest supported flag."
	}
	return "Review the command's supported flags."
}

func (e unknownFlagGuidanceError) CLIHumanNextCommand() string {
	return e.nextHuman
}

func (e unknownFlagGuidanceError) CLIJSONReplacementActions() []cli.JSONAction {
	command := e.replacementJSON
	reason := "Retry with the supported flag --" + e.suggestion + "."
	if command == "" {
		command = e.nextHuman
		reason = "Review the supported flags before retrying."
	}
	return []cli.JSONAction{{
		Command:     command,
		Reason:      reason,
		Destructive: false,
	}}
}

func contextualizeUnknownFlag(root *cobra.Command, err error) error {
	var missing interface {
		error
		GetSpecifiedName() string
		GetSpecifiedShortnames() string
	}
	if !errors.As(err, &missing) {
		return err
	}
	if commandRequestsJSON(os.Args[1:]) {
		cli.JSONOutput = true
	}
	command, _, findErr := root.Find(os.Args[1:])
	if findErr != nil || command == nil {
		command = root
	}
	name := missing.GetSpecifiedName()
	suggestion := ""
	if missing.GetSpecifiedShortnames() == "" {
		suggestion = closestResourceName(name, commandFlagNames(command))
	}
	if suggestion == "" {
		return unknownFlagGuidanceError{
			err:       err,
			nextHuman: command.CommandPath() + " --help",
		}
	}
	corrected := correctedFlagCommand(os.Args[1:], name, suggestion)
	return unknownFlagGuidanceError{
		err:             err,
		suggestion:      suggestion,
		nextHuman:       strings.TrimSuffix(corrected, " --json"),
		replacementJSON: corrected,
	}
}

func commandFlagNames(command *cobra.Command) []string {
	names := make(map[string]bool)
	command.Flags().VisitAll(func(flag *pflag.Flag) {
		if !flag.Hidden {
			names[flag.Name] = true
		}
	})
	command.InheritedFlags().VisitAll(func(flag *pflag.Flag) {
		if !flag.Hidden {
			names[flag.Name] = true
		}
	})
	return sortedKeys(names)
}

func correctedFlagCommand(args []string, requested, suggestion string) string {
	corrected := append([]string{}, args...)
	for index, arg := range corrected {
		if arg == "--"+requested {
			corrected[index] = "--" + suggestion
			break
		}
		if strings.HasPrefix(arg, "--"+requested+"=") {
			corrected[index] = "--" + suggestion + strings.TrimPrefix(arg, "--"+requested)
			break
		}
	}
	parts := []string{"orbit"}
	for _, arg := range corrected {
		parts = append(parts, shellquote.Quote(arg))
	}
	return strings.Join(parts, " ")
}

func commandRequestsJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || arg == "--json=true" {
			return true
		}
	}
	return false
}
