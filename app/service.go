package app

import (
	"fmt"

	"github.com/iml885203/orbit/daemon"
	"github.com/spf13/cobra"
)

func serviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage per-service configuration",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "mode <name> <dev|container>",
		Short: "Switch a dual-defined service between dev and container mode",
		Args:  cobra.ExactArgs(2),
		RunE:  runServiceMode,
	})
	return cmd
}

func validateServiceMode(mode string) error {
	if mode != "dev" && mode != "container" {
		return fmt.Errorf("mode must be 'dev' or 'container', got %q", mode)
	}
	return nil
}

func runServiceMode(_ *cobra.Command, args []string) error {
	name, mode := args[0], args[1]
	if err := validateServiceMode(mode); err != nil {
		return err
	}
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return err
	}
	if err := client.SetServiceMode(name, mode); err != nil {
		return err
	}
	fmt.Printf("✓ %s → %s mode\n", name, mode)
	return nil
}
