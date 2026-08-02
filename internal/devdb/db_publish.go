package devdb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/dbstate"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/iml885203/orbit/internal/sqlpublish"
	"github.com/spf13/cobra"
)

var (
	publishForce    bool
	publishAll      bool
	publishParallel int
	publishYes      bool
)

func dbPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish <database|project> | --all",
		Short: "Publish a SQL project's schema to the configured SQL Server",
		Long: `Build the SQL project on the host and publish the dacpac straight to the
configured SQL Server target — no image rebuild, no container-side tooling.
Idempotent: an unchanged project converges to a no-op in seconds. Data is
preserved; destructive changes are blocked unless --force.

The argument may be a database name or a project name (both appear in
` + "`orbit sqlserver list`" + `); a project publishes each of its databases.

Projects come from sqlserver.projects in the active environment. Requires the
host dotnet SDK and sqlpackage
(` + sqlpublish.InstallHint + `).`,
		Args: func(_ *cobra.Command, args []string) error {
			if publishAll {
				if len(args) != 0 {
					return fmt.Errorf("--all takes no database argument")
				}
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("requires a database name (or --all)")
			}
			return nil
		},
		RunE: runDBPublish,
	}
	cmd.Flags().BoolVar(&publishForce, "force", false, "allow schema changes that can permanently delete data")
	cmd.Flags().BoolVar(&publishAll, "all", false, "publish every configured database")
	cmd.Flags().IntVar(&publishParallel, "parallel", 0, "with --all, publish up to N databases concurrently (0 = sequential); bare --parallel uses 4")
	cmd.Flags().Lookup("parallel").NoOptDefVal = "4"
	cmd.Flags().BoolVarP(&publishYes, "yes", "y", false, "confirm the data-loss risk of --force without prompting")
	return cmd
}

func runDBPublish(_ *cobra.Command, args []string) error {
	if publishYes && !publishForce {
		return fmt.Errorf("--yes requires --force")
	}
	// Input validation outranks the destructive-command recommendation: a
	// malformed argument is a plain input error in every output mode, decided
	// before we reach the --json destructive gate (or dial the daemon).
	if !publishAll && !safeArgName.MatchString(args[0]) {
		return fmt.Errorf("invalid database or project name %q", args[0])
	}
	if err := rejectForcedPublishJSON(args); err != nil {
		return err
	}

	client, err := dialDBWorkflow()
	if err != nil {
		return err
	}

	out := io.Writer(os.Stdout)
	if cli.JSONOutput {
		out = io.Discard
	}

	var published []string
	if publishAll {
		targets, err := runDBPublishAll(client, out)
		if err != nil {
			return err
		}
		published = publishTargetNames(targets)
		return writeDBPublishJSON(args, published)
	}

	// The argument may name a database or a whole project; a project publishes
	// each of its databases (via the same bounded runner as --all).
	resolved, projects, err := resolveDBArgFromClient(client, args[0])
	if err != nil {
		return err
	}
	if resolved.FromProject() {
		targets, err := runDBPublishProject(client, projects, resolved, out)
		if err != nil {
			return err
		}
		published = publishTargetNames(targets)
		return writeDBPublishJSON(args, published)
	}
	if publishParallel > 0 {
		return fmt.Errorf("--parallel applies only to --all or a project with multiple databases")
	}

	dbName := resolved.DBs[0]
	if !safeDBName.MatchString(dbName) {
		return fmt.Errorf("invalid database name %q", dbName)
	}
	opts, err := publishConnOptsFromClient(client, dbName)
	if err != nil {
		return err
	}
	// Reuse the project list resolveDBArgFromClient already fetched rather than
	// re-dialing the daemon for the same data.
	opts.SQLProj, err = sqlProjForDatabaseOrError(projects, dbName)
	if err != nil {
		return err
	}
	opts.Force = publishForce
	if !authorizeForcedPublish([]publishTargetRef{{DB: dbName}}) {
		fmt.Println("Aborted.")
		return nil
	}
	// A publish prepares fast reset state for a database it just created.
	if err := publishOne(client, opts, true, out); err != nil {
		return err
	}
	return writeDBPublishJSON(args, []string{dbName})
}

func writeDBPublishJSON(args, databases []string) error {
	if !cli.JSONOutput {
		return nil
	}
	return writeDBPublishJSONTo(os.Stdout, args, databases)
}

func writeDBPublishJSONTo(out io.Writer, args, databases []string) error {
	return cli.WriteJSONSuccess(out, dbPublishCommand(args, true, false), dbPublishJSONResult{
		Databases:       databases,
		Published:       len(databases),
		DataLossAllowed: false,
	}, nil)
}

func rejectForcedPublishJSON(args []string) error {
	if !cli.JSONOutput || !publishForce {
		return nil
	}
	return cli.NewUnsupportedDestructiveJSONCommandError(
		dbPublishCommand(args, false, false),
		"Run manually; Orbit will show the data-loss scope and ask for confirmation.",
	)
}

func dbPublishCommand(args []string, jsonOutput, includeYes bool) string {
	parts := []string{"orbit", "sqlserver", "publish"}
	if publishAll {
		parts = append(parts, "--all")
	} else if len(args) > 0 {
		parts = append(parts, shellquote.Quote(args[0]))
	}
	if publishParallel > 0 {
		parts = append(parts, fmt.Sprintf("--parallel=%d", publishParallel))
	}
	if publishForce {
		parts = append(parts, "--force")
	}
	if includeYes && publishYes {
		parts = append(parts, "--yes")
	}
	if jsonOutput {
		parts = append(parts, "--json")
	}
	return strings.Join(parts, " ")
}

func publishTargetNames(targets []publishTargetRef) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.DB)
	}
	return names
}

// runDBPublishProject publishes every database a project owns, reusing the
// same bounded runner as --all but scoped to that project's targets. projects
// is the list resolveDBArgFromClient already fetched — reused, not re-dialed.
func runDBPublishProject(client *daemon.Client, projects []DevDBProject, resolved resolvedArg, out io.Writer) ([]publishTargetRef, error) {
	targets := publishTargetsForDBs(projects, resolved.DBs)
	if len(targets) == 0 {
		return nil, fmt.Errorf("project %q has no resolvable databases — check `orbit sqlserver list`", resolved.Project)
	}
	if !authorizeForcedPublish(targets) {
		fmt.Println("Aborted.")
		return nil, nil
	}
	if err := runPublishTargets(client, targets, resolved.Project, out); err != nil {
		return nil, err
	}
	return targets, nil
}

// runDBPublishAll publishes every database the project merge knows.
func runDBPublishAll(client *daemon.Client, out io.Writer) ([]publishTargetRef, error) {
	targets, err := fetchAllPublishTargets(client)
	if err != nil {
		return nil, err
	}
	if !authorizeForcedPublish(targets) {
		fmt.Println("Aborted.")
		return nil, nil
	}
	if err := runPublishTargets(client, targets, "", out); err != nil {
		return nil, err
	}
	return targets, nil
}

func authorizeForcedPublish(targets []publishTargetRef) bool {
	if !publishForce || publishYes {
		return true
	}
	return cli.Confirm(forcedPublishPrompt(targets))
}

func forcedPublishPrompt(targets []publishTargetRef) string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.DB)
	}
	scope := strings.Join(names, ", ")
	return fmt.Sprintf(
		"Force-publish %s? This allows schema changes that can permanently delete data.",
		scope,
	)
}

// runPublishTargets is the shared body of the multi-target publish paths
// (--all and a single project): resolve the base conn, run the bounded
// publisher, report progress. scope names the project for the project path;
// empty means every configured project ("all").
func runPublishTargets(client *daemon.Client, targets []publishTargetRef, scope string, out io.Writer) error {
	base, err := publishConnOptsFromClient(client, "")
	if err != nil {
		return err
	}
	base.Force = publishForce
	if scope == "" {
		fmt.Fprintf(out, "Publishing %d databases → %s:%d\n", len(targets), base.Host, base.Port)
	} else {
		fmt.Fprintf(out, "Publishing %s (%d databases) → %s:%d\n", scope, len(targets), base.Host, base.Port)
	}
	if err := publishRun(client, base, targets, "published", true, publishParallel, out); err != nil {
		return err
	}
	if scope == "" {
		fmt.Fprintf(out, "\n%s Published all %d databases\n", cli.Green.Sprint("✓"), len(targets))
	} else {
		fmt.Fprintf(out, "\n%s Published %d databases in %s\n", cli.Green.Sprint("✓"), len(targets), scope)
	}
	return nil
}

// publishSequentially publishes targets in order, stopping at the first
// failure — a broken schema early in the list is a reason to look, not
// to plough on. Per-run behavior (Force, IncludeComposite) rides base.
// baselineOnCreate lets freshly created DBs prepare fast reset state.
// The daemon's runPublishOp deliberately
// keeps its own loop: its output sink, event source and failure contract
// (SSE done frames) all differ.
func publishSequentially(client *daemon.Client, base sqlpublish.Opts, targets []publishTargetRef, failVerb string, baselineOnCreate bool, out io.Writer) error {
	for i, t := range targets {
		fmt.Fprintf(out, "\n[%d/%d] %s\n", i+1, len(targets), t.DB)
		opts := base
		opts.DB = t.DB
		opts.SQLProj = t.SQLProj
		if err := publishOne(client, opts, baselineOnCreate, out); err != nil {
			return fmt.Errorf("%s failed (%d of %d %s, the rest skipped): %w", t.DB, i, len(targets), failVerb, err)
		}
	}
	return nil
}

// publishConcurrently publishes targets with bounded concurrency, each
// into its own buffer via publishOne. It is a thin adapter over
// runBoundedPublish (which owns the concurrency mechanics). Opt-in only
// (`--parallel`) and intended for an already prepared server,
// because concurrent first-time publishes racing to create the same
// shared (composite) objects can conflict.
func publishConcurrently(client *daemon.Client, base sqlpublish.Opts, targets []publishTargetRef, failVerb string, baselineOnCreate bool, concurrency int, out io.Writer) error {
	return runBoundedPublish(targets, concurrency, failVerb, out, func(t publishTargetRef) (string, error) {
		var buf bytes.Buffer
		opts := base
		opts.DB = t.DB
		opts.SQLProj = t.SQLProj
		err := publishOne(client, opts, baselineOnCreate, &buf)
		return buf.String(), err
	})
}

// runBoundedPublish runs work over targets with concurrency workers,
// flushing each target's output as one labelled block on completion (in
// completion order, under a mutex — parallel streams would interleave)
// and, unlike the sequential path, letting every target finish and
// reporting all failures together. Split from publishConcurrently so the
// semaphore/WaitGroup/mutex mechanics are unit-testable apart from the
// real publish. concurrency must be >= 1 (publishRun enforces it before
// dispatching here).
func runBoundedPublish(targets []publishTargetRef, concurrency int, failVerb string, out io.Writer, work func(publishTargetRef) (string, error)) error {
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	var failures []string

	for _, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(t publishTargetRef) {
			defer wg.Done()
			defer func() { <-sem }()
			output, err := work(t)

			mu.Lock()
			defer mu.Unlock()
			done++
			status := cli.Green.Sprint("✓")
			if err != nil {
				status = cli.Red.Sprint("✗")
				failures = append(failures, fmt.Sprintf("%s: %v", t.DB, err))
			}
			fmt.Fprintf(out, "\n[%d/%d] %s %s\n%s", done, len(targets), status, t.DB, output)
		}(t)
	}
	wg.Wait()

	if len(failures) > 0 {
		return fmt.Errorf("%d of %d %s failed:\n%s", len(failures), len(targets), failVerb, strings.Join(failures, "\n"))
	}
	return nil
}

// publishRun dispatches a multi-target publish to the sequential or the
// bounded-concurrent runner. concurrency <= 0 means sequential.
func publishRun(client *daemon.Client, base sqlpublish.Opts, targets []publishTargetRef, failVerb string, baselineOnCreate bool, concurrency int, out io.Writer) error {
	if concurrency > 0 {
		return publishConcurrently(client, base, targets, failVerb, baselineOnCreate, concurrency, out)
	}
	return publishSequentially(client, base, targets, failVerb, baselineOnCreate, out)
}

// publishOne runs one publish and posts the dbstate event — the
// per-database unit the single and --all paths share.
// Force/IncludeComposite arrive on opts; clean is explicit because it
// changes the operation (revert + publish + baseline refresh), not just
// a sqlpackage property. baselineOnCreate declares a fresh, clean
// baseline when the publish brought the DB into existence — mirrors the
// daemon's autoBaseline so both surfaces stay consistent. Bootstrap
// passes false: it baselines every target in its own pass.
func publishOne(client *daemon.Client, opts sqlpublish.Opts, baselineOnCreate bool, out io.Writer) error {
	dbName := opts.DB
	fmt.Fprintf(out, "Publishing %s from %s → %s:%d\n", dbName, opts.SQLProj, opts.Host, opts.Port)
	res := runSQLPublish(opts, false, out)
	if !res.OK {
		postDBStateEvent(client, "publish", dbName, dbstate.SourceCLI, "error", res.DurationMs, res.Err.Error())
		if res.Code == sqlpublish.CodePublishBlockedDataLoss {
			return fmt.Errorf("publish blocked: possible data loss — rerun with --force to override: %w", res.Err)
		}
		return res.Err
	}
	postDBStateEvent(client, "publish", dbName, dbstate.SourceCLI, "ok", res.DurationMs, "")
	fmt.Fprintf(out, "%s Published %s in %.1fs\n", cli.Green.Sprint("✓"), dbName, float64(res.DurationMs)/1000)

	if baselineOnCreate && res.Created {
		// The DB was just created, so it is clean — declare the baseline
		// reset reverts to, without the user ever running snapshot.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := sqlpublish.RefreshBaseline(ctx, opts, dbName, out); err != nil {
			fmt.Fprintf(out, "%s Reset preparation for %s could not complete: %v\n", cli.Yellow.Sprint("!"), dbName, err)
		} else {
			postDBStateEvent(client, "snapshot", dbName, dbstate.SourceCLI, "ok", 0, "")
		}
	}
	return nil
}
