package devdb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/spf13/cobra"
)

var dacpacDir string

// dacpacDirGiven records that --dacpac-dir appeared on the command line, as
// opposed to being left at its empty default. A caller that builds the
// argument from a variable can pass an empty one — CI signalling "no
// artifacts this run" by clearing an env var is the case that prompted this
// — and reading that as "flag omitted" quietly builds from source, which
// then fails on project paths that do not exist on that machine. The
// argument was the mistake; the error should say so.
//
// Set in the flag's own parse callback so it belongs to whichever command is
// running, rather than to whichever registered last.
var dacpacDirGiven bool

func addDacpacDirFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&dacpacDir, "dacpac-dir", "", "use prebuilt dacpacs from this per-project artifact root")
	cmd.PreRun = func(c *cobra.Command, _ []string) {
		dacpacDirGiven = c.Flags().Changed("dacpac-dir")
	}
}

func invocationDacpacDir() (string, error) {
	if strings.TrimSpace(dacpacDir) == "" {
		if dacpacDirGiven {
			return "", fmt.Errorf("--dacpac-dir was given an empty path; omit the flag to build from source")
		}
		return "", nil
	}
	root, err := filepath.Abs(dacpacDir)
	if err != nil {
		return "", fmt.Errorf("resolve --dacpac-dir: %w", err)
	}
	return root, nil
}

func SQLServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sqlserver",
		Short: "Manage SQL Server Database Projects",
		Long: `Manage SQL Server Database Projects declared by the active environment.

Examples:
  orbit sqlserver list                        # show configured projects and databases
  orbit sqlserver query "SELECT @@VERSION"    # query the configured SQL Server
  orbit sqlserver diff SampleDB               # check pending schema changes
  orbit sqlserver publish SampleDB            # push schema changes, preserving data
  orbit sqlserver reset SampleDB              # discard local data, return to a clean state`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown SQL Server command %q", args[0])
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
		return cli.WriteJSONSuccess(os.Stdout, "orbit sqlserver list", resp, nil)
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
