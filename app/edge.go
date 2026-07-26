package app

import (
	"fmt"

	"github.com/iml885203/orbit/daemon"
	"github.com/spf13/cobra"
)

func edgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "edge",
		Short:  "Manage dependency edges between services",
		Hidden: true,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "detach <from> <to>",
		Short: "Mark the edge from→to as detached (orchestrator ignores it for startup ordering)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return runEdgeSet(args[0], args[1], true)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "attach <from> <to>",
		Short: "Re-attach the edge from→to so the orchestrator honours it again",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return runEdgeSet(args[0], args[1], false)
		},
	})
	return cmd
}

func runEdgeSet(from, to string, detached bool) error {
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return err
	}
	if err := client.SetEdgeDetached(from, to, detached); err != nil {
		return err
	}
	verb := "attached"
	if detached {
		verb = "detached"
	}
	fmt.Printf("✓ %s → %s %s\n", from, to, verb)
	return nil
}
