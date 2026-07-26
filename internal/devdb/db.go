package devdb

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/iml885203/orbit/cli"
	"github.com/spf13/cobra"
)

func DBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Manage local databases via host-side schema publish",
		Long: `Manage local databases via host-side schema publish. The projects come
from the team-shared allowlist (envs/data/db-projects.yaml) — one list for
every env, matched case-insensitively against your workspace folders.

Examples:
  orbit db list                        # show the allowlisted projects and databases
  orbit db diff SampleDB               # check pending schema changes
  orbit db publish SampleDB            # push schema changes, preserving data
  orbit db reset SampleDB              # discard local data, return to a clean state`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown database command %q", args[0])
			}
			return cmd.Help()
		},
	}
	cmd.AddCommand(dbListCmd())
	cmd.AddCommand(dbDiffCmd())
	cmd.AddCommand(dbPublishCmd())
	cmd.AddCommand(dbResetCmd())
	return cmd
}

func dbListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the allowlisted SQL projects and their databases",
		RunE:  runDBList,
	}
}

func runDBList(_ *cobra.Command, _ []string) error {
	client, err := dialDBWorkflow()
	if err != nil {
		return err
	}

	resp, err := fetchDevDBProjects(client)
	if err != nil {
		return err
	}

	if cli.JSONOutput {
		return printJSON(resp)
	}

	if len(resp.Projects) == 0 {
		fmt.Println("No SQL projects found. Add them to the shared allowlist (envs/data/db-projects.yaml) and check the folders exist in your workspace.")
		return nil
	}
	for _, p := range resp.Projects {
		_, _ = cli.Bold.Println(p.Name)
		_, _ = cli.Faint.Printf("  %s\n", p.Path)
		if len(p.Databases) == 0 {
			fmt.Println("  (no databases)")
			continue
		}
		for _, db := range p.Databases {
			fmt.Printf("  - %s\n", db)
		}
	}
	return nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
