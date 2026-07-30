package devdb

// The `db diff` CLI command: show how a database's schema differs from
// its SQL project — i.e. what `orbit sqlserver publish <db>` would change.
// Read-only: it builds the project's dacpac and asks sqlpackage for a
// deploy report (or, with --script, the T-SQL a publish would run). The
// database is never touched, so unlike publish/reset/snapshot this
// command fully supports --json.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/sqlpublish"
	"github.com/spf13/cobra"
)

var (
	diffScript  bool
	diffAnalyze bool
)

func dbDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <database|project>",
		Short: "Show how a database differs from its SQL project (what publish would change)",
		Long: `Compare a database against its SQL project and report the schema
changes a publish would apply — objects to create, alter, or drop, plus
any possible-data-loss warnings. Read-only: the database is not modified.

The argument may be a database name or a project name (both appear in
` + "`orbit sqlserver list`" + `); a project diffs each of its databases.

By default prints a human-readable summary. Use --script to print the
exact T-SQL a publish would run, or --json for structured output.

The default check is optimized for the common edit-and-publish loop. When
source files changed, it names them immediately. Use --analyze to inspect
the affected database objects and possible data-loss warnings.

Requires the host dotnet SDK and sqlpackage.`,
		Args: cobra.ExactArgs(1),
		RunE: runDBDiff,
	}
	cmd.Flags().BoolVar(&diffScript, "script", false, "print the full T-SQL deployment script instead of a summary")
	cmd.Flags().BoolVar(&diffAnalyze, "analyze", false, "inspect affected database objects and possible data loss")
	return cmd
}

func runDBDiff(_ *cobra.Command, args []string) error {
	if !safeArgName.MatchString(args[0]) {
		return fmt.Errorf("invalid database or project name %q", args[0])
	}
	client, err := dialDBWorkflow()
	if err != nil {
		return err
	}

	// The argument may name a database or a whole project; a project expands
	// to each of its databases (diff is read-only, so a fan-out is safe). The
	// resolved project list is reused per-database below, not re-dialed.
	resolved, projects, err := resolveDBArgFromClient(client, args[0])
	if err != nil {
		return err
	}

	if len(resolved.DBs) == 1 {
		return diffOneDB(client, projects, resolved.DBs[0])
	}
	return diffProjectDBs(client, projects, resolved)
}

// diffProjectDBs diffs every database a project owns. --script and --json are
// per-database commands, so a multi-database project rejects both rather than
// guess how to combine them; the human summary prints each in turn.
func diffProjectDBs(client *daemon.Client, projects []DevDBProject, resolved resolvedArg) error {
	if diffScript {
		return errNeedsSingleDB("--script", resolved)
	}
	if cli.JSONOutput {
		return errNeedsSingleDB("--json", resolved)
	}
	fmt.Printf("%s (%d databases)\n\n", cli.Bold.Sprint(resolved.Project), len(resolved.DBs))
	for i, db := range resolved.DBs {
		if i > 0 {
			fmt.Println()
		}
		if err := diffOneDB(client, projects, db); err != nil {
			return err
		}
	}
	return nil
}

// errNeedsSingleDB is the shared "this flag can't run against a whole project"
// message for --script / --json.
func errNeedsSingleDB(flag string, resolved resolvedArg) error {
	return fmt.Errorf("%s needs a single database; %q is a project with %d (%s)",
		flag, resolved.Project, len(resolved.DBs), strings.Join(resolved.DBs, ", "))
}

// diffOneDB runs the diff for a single database — the original command body.
// projects is the already-fetched list; the .sqlproj is resolved from it
// rather than re-dialing the daemon per database.
func diffOneDB(client *daemon.Client, projects []DevDBProject, dbName string) error {
	if !safeDBName.MatchString(dbName) {
		return fmt.Errorf("invalid database name %q", dbName)
	}
	opts, err := publishConnOptsFromClient(client, dbName)
	if err != nil {
		return err
	}
	opts.SQLProj, err = sqlProjForDatabaseOrError(projects, dbName)
	if err != nil {
		return err
	}
	opts.Analyze = diffAnalyze

	// Build output goes to stderr so stdout stays clean for --json / --script.
	buildOut := os.Stderr

	if diffScript {
		var script string
		var runErr error
		res := withPublishScratch(opts, func(ctx context.Context, o sqlpublish.Opts) sqlpublish.Result {
			s, code, e := sqlpublish.DiffScript(ctx, o, buildOut)
			script = s
			runErr = e
			return sqlpublish.Result{OK: e == nil, Code: code, Err: e}
		})
		if !res.OK {
			return runErr
		}
		if cli.JSONOutput {
			return cli.WriteJSONSuccess(os.Stdout, "orbit sqlserver diff --script",
				map[string]any{"db": dbName, "script": script}, nil)
		}
		fmt.Print(script)
		return nil
	}

	var result sqlpublish.DiffResult
	var runErr error
	res := withPublishScratch(opts, func(ctx context.Context, o sqlpublish.Opts) sqlpublish.Result {
		r, code, e := sqlpublish.Diff(ctx, o, buildOut)
		result = r
		runErr = e
		return sqlpublish.Result{OK: e == nil, Code: code, Err: e}
	})
	if !res.OK {
		return runErr
	}

	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, "orbit sqlserver diff", result, nil)
	}
	printDiffSummary(result)
	return nil
}

// printDiffSummary renders the human-facing diff: a headline, a
// data-loss warning block if any, then the operations grouped
// Drop → Alter → Create (already sorted by sqlpublish.Diff).
func printDiffSummary(r sqlpublish.DiffResult) {
	if r.InSync {
		if r.Quick {
			fmt.Printf("%s %s is in sync — no changes since the last publish\n", cli.Green.Sprint("✓"), r.DB)
		} else {
			fmt.Printf("%s %s is in sync — no schema changes\n", cli.Green.Sprint("✓"), r.DB)
		}
		return
	}

	// File-level fast answer: names what moved; the engine names the
	// exact operations.
	if len(r.FileChanges) > 0 {
		fmt.Printf("%s %s: %s\n", cli.Yellow.Sprint("≠"), r.DB, r.Summary())
		fmt.Printf("\n%s\n", cli.Bold.Sprint("Changed files"))
		for _, c := range r.FileChanges {
			fmt.Printf("  %s %-8s %s\n", opMark(c.Action), strings.ToLower(c.Action), c.Path)
		}
		fmt.Printf("\nAnalyze database impact: orbit sqlserver diff %s --analyze\n", r.DB)
		return
	}

	headline := r.Summary()
	if r.Cached {
		headline += cli.Faint.Sprint("  (previously analyzed)")
	}
	fmt.Printf("%s %s: %s\n", cli.Yellow.Sprint("≠"), r.DB, headline)

	if r.DataLoss {
		fmt.Printf("\n%s\n", cli.Red.Sprint("Possible data loss:"))
		for _, a := range r.Alerts {
			fmt.Printf("  %s %s\n", cli.Red.Sprint("!"), a.Message)
		}
	}

	fmt.Printf("\n%s\n", cli.Bold.Sprint("Changes"))
	for _, op := range r.Ops {
		mark := opMark(op.Action)
		flag := ""
		if op.DataLoss {
			flag = cli.Red.Sprint(" (data loss)")
		}
		fmt.Printf("  %s %-6s %s %s%s\n", mark, op.Action, shortType(op.ObjectType), op.Name, flag)
	}
}

// opMark colours a change by destructiveness. The action vocabulary
// (engine ops AND file changes) lives in one place —
// sqlpublish.ActionRank — so a new action value can't desync ordering
// from colour.
func opMark(action string) string {
	switch sqlpublish.ActionRank(action) {
	case 0:
		return cli.Red.Sprint("−")
	case 1:
		return cli.Yellow.Sprint("~")
	case 2:
		return cli.Green.Sprint("+")
	default:
		return "?"
	}
}

// shortType trims sqlpackage's "Sql" prefix from object type names
// (SqlTable → Table, SqlProcedure → Procedure) for readability.
func shortType(t string) string {
	if len(t) > 3 && t[:3] == "Sql" {
		return t[3:]
	}
	return t
}
