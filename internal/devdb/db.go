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
		Short: "Manage SQL Server Database Projects",
		Long: `Manage SQL Server Database Projects declared by the active environment.

Examples:
  orbit db list                        # show configured projects and databases
  orbit db query "SELECT @@VERSION"    # query the configured SQL Server
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
	cmd.AddCommand(dbQueryCmd())
	cmd.AddCommand(dbDiffCmd())
	cmd.AddCommand(dbPublishCmd())
	cmd.AddCommand(dbResetCmd())
	return cmd
}

func dbListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured SQL projects and their databases",
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
		fmt.Println("No SQL projects configured. Add .sqlproj paths to sqlserver.projects in the active environment.")
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
