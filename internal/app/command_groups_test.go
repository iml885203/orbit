package app

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/spf13/cobra"
)

func commandTreeWith(groups ...*cobra.Command) *cobra.Command {
	root := &cobra.Command{Use: "orbit"}
	root.AddCommand(groups...)
	rejectUnknownSubcommands(root)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SilenceErrors = true
	root.SilenceUsage = true
	return root
}

// A removed or misspelled subcommand must fail, not print help and exit 0:
// scripts would otherwise report success while nothing ran.
func TestCommandGroupRejectsAnUnknownSubcommand(t *testing.T) {
	root := commandTreeWith(envCmd(), sourceCmd())
	for _, args := range [][]string{{"env", "sync"}, {"source", "synk"}} {
		root.SetArgs(args)
		err := root.Execute()
		if !errors.Is(err, cli.ErrInvalidArgument) {
			t.Fatalf("orbit %s error = %v, want invalid argument", strings.Join(args, " "), err)
		}
		if !strings.Contains(err.Error(), args[1]) {
			t.Fatalf("orbit %s error = %q, want it to name %q", strings.Join(args, " "), err, args[1])
		}
	}
}

func TestCommandGroupSuggestsTheClosestSubcommand(t *testing.T) {
	root := commandTreeWith(sourceCmd())
	root.SetArgs([]string{"source", "synk"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "did you mean sync") {
		t.Fatalf("error = %v, want a suggestion of sync", err)
	}
}

func TestBareCommandGroupStillShowsHelp(t *testing.T) {
	root := commandTreeWith(envCmd())
	root.SetArgs([]string{"env"})
	if err := root.Execute(); err != nil {
		t.Fatalf("bare `orbit env` = %v, want its help", err)
	}
}
