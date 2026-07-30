package devdb

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func dbQueryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query [sql]",
		Short: "Query the configured SQL Server",
		Long: `Run sqlcmd inside the explicitly configured SQL Server target.

Examples:
  orbit sqlserver query "SELECT TOP 5 * FROM Users"
  orbit sqlserver query                              # interactive mode`,
		Args: cobra.ArbitraryArgs,
		RunE: runDBQuery,
	}
}

func runDBQuery(_ *cobra.Command, args []string) error {
	client, err := dialDBWorkflow()
	if err != nil {
		return err
	}
	meta, err := fetchDevDBMeta(client)
	if err != nil {
		return err
	}
	if meta.SQLServerTarget == "" || meta.SQLServerUsername == "" || meta.SQLServerPasswordEnv == "" {
		return fmt.Errorf("SQL Server query settings unavailable from the active environment")
	}

	commandArgs := dbQueryDockerArgs(meta, args)
	command := exec.Command("docker", commandArgs...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("querying configured SQL Server target: %w", err)
	}
	return nil
}

func dbQueryDockerArgs(meta *DevDBMetaResponse, queryParts []string) []string {
	const runSQLCmd = `password="$(printenv "$1")"
if [ -z "$password" ]; then
  echo "$1 is empty in the configured SQL Server target" >&2
  exit 2
fi
export SQLCMDPASSWORD="$password"
if [ "$#" -ge 3 ]; then
  exec /opt/mssql-tools18/bin/sqlcmd -S localhost -U "$2" -C -I -Q "$3"
fi
exec /opt/mssql-tools18/bin/sqlcmd -S localhost -U "$2" -C -I`

	commandArgs := []string{
		"exec", "-i", meta.SQLServerTarget,
		"/bin/sh", "-c", runSQLCmd,
		"orbit-db-query",
		meta.SQLServerPasswordEnv,
		meta.SQLServerUsername,
	}
	if len(queryParts) > 0 {
		commandArgs = append(commandArgs, strings.Join(queryParts, " "))
	}
	return commandArgs
}
