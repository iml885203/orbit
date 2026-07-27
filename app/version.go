package app

import (
	"fmt"

	"github.com/iml885203/orbit/cli"
	"github.com/spf13/cobra"
)

type versionJSONData struct {
	Version string `json:"version"`
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the Orbit version",
		Args:  cobra.NoArgs,
		RunE:  runVersion,
	}
}

func runVersion(cmd *cobra.Command, _ []string) error {
	current := buildVersion()
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(
			cmd.OutOrStdout(),
			commandString(),
			versionJSONData{Version: current},
			nil,
		)
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), current)
	return err
}
