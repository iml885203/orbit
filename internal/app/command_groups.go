package app

import (
	"fmt"
	"strings"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/spf13/cobra"
)

// rejectUnknownSubcommands makes every command group (a command with
// subcommands and no action of its own) fail on a word it does not know.
// Cobra otherwise prints the group's help and exits 0, so a script calling a
// removed or misspelled subcommand such as `orbit env sync` would report
// success while doing nothing. Bare `orbit env` still shows the help.
func rejectUnknownSubcommands(root *cobra.Command) {
	for _, group := range root.Commands() {
		markUnknownSubcommandsInvalid(group)
	}
}

func markUnknownSubcommandsInvalid(cmd *cobra.Command) {
	if !cmd.HasSubCommands() {
		return
	}
	if cmd.Run == nil && cmd.RunE == nil {
		if cmd.SuggestionsMinimumDistance <= 0 {
			cmd.SuggestionsMinimumDistance = 2
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return unknownSubcommandError(cmd, args[0])
		}
	}
	for _, child := range cmd.Commands() {
		markUnknownSubcommandsInvalid(child)
	}
}

func unknownSubcommandError(group *cobra.Command, word string) error {
	message := fmt.Sprintf("unknown command %q for %q", word, group.CommandPath())
	if suggestions := group.SuggestionsFor(word); len(suggestions) > 0 {
		message += " (did you mean " + strings.Join(suggestions, " or ") + "?)"
	}
	return cli.NewInvalidArgumentError(message)
}
