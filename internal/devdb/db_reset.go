package devdb

// The `db reset` CLI command: the second of the two user-facing verbs
// (publish / reset). Reset discards local data and returns a database to
// a clean state at the latest schema. It rides sqlpublish.Reset, which
// hides the baseline machinery — the user never types snapshot.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/dbstate"
	"github.com/iml885203/orbit/internal/sqlpublish"
	"github.com/spf13/cobra"
)

var resetYes bool

func dbResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset <database|project>",
		Short: "Discard local data and return a database to a clean state at the latest schema",
		Long: `Reset returns a database to a clean state at the latest schema, discarding
local data changes (test data, ad-hoc edits) and disconnecting active clients.
When no clean baseline exists, reset drops and recreates the whole database
before publishing the latest schema; the confirmation prompt identifies that path.

The argument may be a database name or a project name (both appear in
` + "`orbit sqlserver list`" + `), but reset acts on one database: a project with
more than one database is rejected — name the specific database.
Each database name must map to exactly one project. To use one schema for
multiple databases, declare their names on one sqlserver.projects entry, then
reset a specific database by name.

Destructive: run manually. Requires sqlpackage. The host dotnet SDK and project
sources are also required unless --dacpac-dir supplies prebuilt artifacts.`,
		Args: cobra.ExactArgs(1),
		RunE: runDBReset,
	}
	cmd.Flags().BoolVarP(&resetYes, "yes", "y", false, "confirm discarding local data without prompting")
	addDacpacDirFlag(cmd)
	return cmd
}

func runDBReset(_ *cobra.Command, args []string) error {
	// Malformed argument is a plain input error, decided before the --json
	// destructive gate and before dialing the daemon.
	if !safeArgName.MatchString(args[0]) {
		return fmt.Errorf("invalid database or project name %q", args[0])
	}
	if cli.JSONOutput {
		return cli.NewUnsupportedDestructiveJSONCommandError("orbit sqlserver reset "+args[0],
			"Discards local data and disconnects clients; run manually.")
	}

	client, err := dialDBWorkflow()
	if err != nil {
		return err
	}

	// The argument may name a database or a project, but reset acts on exactly
	// one database — a multi-database project is rejected (never fanned out) so
	// a broad argument can't drop several databases at once.
	projects, err := fetchDevDBProjects(client)
	if err != nil {
		return err
	}
	dbName, err := resolveSingleDBArg(projects.Projects, args[0])
	if err != nil {
		return err
	}
	if !safeDBName.MatchString(dbName) {
		return fmt.Errorf("invalid database name %q", dbName)
	}
	opts, err := publishConnOptsFromClient(client, dbName)
	if err != nil {
		return err
	}
	// Reuse the project list fetched above rather than re-dialing the daemon.
	opts.SQLProj, err = sqlProjForDatabaseOrError(projects.Projects, dbName)
	if err != nil {
		return err
	}
	opts.DacpacDir, err = invocationDacpacDir()
	if err != nil {
		return err
	}
	if err := sqlpublish.ValidateDacpacArtifacts(opts.DacpacDir, opts.SQLProj); opts.DacpacDir != "" && err != nil {
		return err
	}

	// One baseline probe drives both the warning and the allowRecreate
	// authorization, so the user is warned about exactly the path that
	// will run. Reset re-checks and refuses to escalate if the baseline
	// vanishes in between.
	recreate := resetIsRecreate(opts)
	if !resetYes && !confirmReset(dbName, recreate) {
		fmt.Println("Aborted.")
		return nil
	}
	return resetOne(client, opts, recreate)
}

// resetIsRecreate reports whether reset will rebuild from scratch (no
// baseline) rather than revert to one. A probe error is treated as
// standard — Reset itself re-checks and refuses to escalate, so an
// unknown state never authorizes a rebuild.
func resetIsRecreate(opts sqlpublish.Opts) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	has, err := sqlpublish.BaselineExists(ctx, opts, opts.DB)
	return err == nil && !has
}

// confirmReset prompts for the destructive intent, warning harder for
// the recreate path (drops and recreates the whole database) than the
// revert path. A non-y answer (or no TTY) aborts. Skipped when --yes.
func confirmReset(db string, recreate bool) bool {
	return cli.Confirm(resetConfirmationPrompt(db, recreate))
}

func resetConfirmationPrompt(db string, recreate bool) string {
	if recreate {
		return fmt.Sprintf("No baseline exists for %s. Reset will DROP AND RECREATE the database, then publish the latest schema. All data will be lost. Continue?", db)
	}
	return fmt.Sprintf("Reset %s? This disconnects clients, discards local data, and applies the latest schema.", db)
}

// resetOne runs the reset and records the outcome: the reset event plus,
// on success, the clean-publish state (latest schema published, baseline
// refreshed) so the dashboard shows the DB as published with a baseline.
func resetOne(client *daemon.Client, opts sqlpublish.Opts, allowRecreate bool) error {
	dbName := opts.DB
	fmt.Printf("Resetting %s → clean at latest schema on %s:%d\n", dbName, opts.Host, opts.Port)

	res := runSQLReset(opts, allowRecreate, os.Stdout)
	if !res.OK {
		postDBStateEvent(client, "reset", dbName, dbstate.SourceCLI, "error", res.DurationMs, res.Err.Error())
		return res.Err
	}
	postDBStateEvent(client, "reset", dbName, dbstate.SourceCLI, "ok", res.DurationMs, "")
	postDBStateEvent(client, "publish_clean", dbName, dbstate.SourceCLI, "ok", res.DurationMs, "")
	fmt.Printf("%s Reset %s in %.1fs\n", cli.Green.Sprint("✓"), dbName, float64(res.DurationMs)/1000)
	return nil
}
