package sqlpublish

// SQL Server database-snapshot lifecycle. One baseline snapshot per database
// (<db>_baseline);
// reverting to it is a seconds-scale replacement for image-based
// resets. The invariant callers must hold: the baseline is only
// created/refreshed when the database contents are known clean
// (bootstrap end, or the tail of a clean publish) — a plain publish
// never touches it.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// safeIdent guards identifiers interpolated into DDL — snapshot
// statements cannot be parameterized, so names must be validated even
// though callers validate too (defense in depth for a generic package).
var safeIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ErrBaselineMissing marks a clean publish attempted before any
// baseline snapshot was declared for the database.
var ErrBaselineMissing = errors.New("no saved clean state")

// BaselineName returns the snapshot database name for db.
func BaselineName(db string) string { return db + "_baseline" }

// BaselineExists reports whether db's baseline snapshot exists.
func BaselineExists(ctx context.Context, opts Opts, db string) (bool, error) {
	if !safeIdent.MatchString(db) {
		return false, fmt.Errorf("invalid database name %q", db)
	}
	conn, err := openMasterDB(opts)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()

	var one int
	err = conn.QueryRowContext(ctx,
		`SELECT 1 FROM sys.databases WHERE name = @p1 AND source_database_id = DB_ID(@p2)`,
		BaselineName(db), db).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("querying baseline snapshot: %w", err)
	}
	return true, nil
}

// RefreshBaseline drops and recreates db's baseline snapshot from its
// current state. Callers own the clean-data invariant.
func RefreshBaseline(ctx context.Context, opts Opts, db string, out io.Writer) error {
	if !safeIdent.MatchString(db) {
		return fmt.Errorf("invalid database name %q", db)
	}
	conn, err := openMasterDB(opts)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	snap := BaselineName(db)
	fmt.Fprintf(out, "[reset] saving clean state for %s\n", db)

	// Snapshot names can't be renamed, so an atomic swap is impossible —
	// instead prove a snapshot CAN be created (temp name) before dropping
	// the existing recovery point, then cut the real one. Never leaves
	// zero baselines behind a failure.
	tmp := snap + "_next"
	if _, err := conn.ExecContext(ctx, "DROP DATABASE IF EXISTS ["+tmp+"]"); err != nil {
		return fmt.Errorf("clearing stale temp snapshot: %w", err)
	}
	if err := createSnapshot(ctx, conn, db, tmp); err != nil {
		return fmt.Errorf("pre-validating snapshot creation: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "DROP DATABASE IF EXISTS ["+snap+"]"); err != nil {
		_, _ = conn.ExecContext(ctx, "DROP DATABASE IF EXISTS ["+tmp+"]")
		return fmt.Errorf("replacing the previous clean state: %w", err)
	}
	if err := createSnapshot(ctx, conn, db, snap); err != nil {
		// Rare window: the temp snapshot proved creation possible moments
		// ago, so this failure is transient. Rerunning the refresh
		// self-heals (it clears the stale temp and retries the whole
		// sequence). The temp snapshot is NOT read by any workflow path —
		// it only preserves this state for manual RESTORE until the rerun.
		return fmt.Errorf("preparing the clean reset state: %w — rerun `orbit sqlserver reset %s`; until then %s preserves the last known clean state (manual recovery: RESTORE DATABASE [%s] FROM DATABASE_SNAPSHOT = '%s')", err, db, tmp, db, tmp)
	}
	if _, err := conn.ExecContext(ctx, "DROP DATABASE IF EXISTS ["+tmp+"]"); err != nil {
		fmt.Fprintf(out, "[reset] warning: temporary clean state %s was not removed: %v\n", tmp, err)
	}
	fmt.Fprintf(out, "[reset] clean state ready for %s\n", db)
	return nil
}

// createSnapshot creates snapshot name from db's current state, laying
// sparse files next to the data files.
func createSnapshot(ctx context.Context, conn *sql.DB, db, name string) error {
	rows, err := conn.QueryContext(ctx,
		`SELECT name, physical_name FROM sys.master_files WHERE database_id = DB_ID(@p1) AND type = 0`, db)
	if err != nil {
		return fmt.Errorf("listing data files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var clauses []string
	for rows.Next() {
		var logical, physical string
		if err := rows.Scan(&logical, &physical); err != nil {
			return err
		}
		if !safeIdent.MatchString(logical) {
			return fmt.Errorf("data file logical name %q is not snapshot-safe", logical)
		}
		// SQL Server runs on Linux in the container: forward-slash paths.
		// The full FILENAME is a SQL string literal: escape embedded
		// quotes (metadata-controlled, but an SA connection deserves the
		// paranoia — and legitimately quoted paths must work).
		dir := path.Dir(strings.ReplaceAll(physical, `\`, "/"))
		filename := strings.ReplaceAll(fmt.Sprintf("%s/%s_%s.ss", dir, name, logical), "'", "''")
		clauses = append(clauses, fmt.Sprintf("(NAME = [%s], FILENAME = '%s')", logical, filename))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(clauses) == 0 {
		return fmt.Errorf("database %q has no data files (is it attached yet?)", db)
	}

	stmt := fmt.Sprintf("CREATE DATABASE [%s] ON %s AS SNAPSHOT OF [%s]", name, strings.Join(clauses, ", "), db)
	if _, err := conn.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("creating snapshot %s: %w", name, err)
	}
	return nil
}

// RevertToBaseline restores db from its baseline snapshot, kicking
// active connections (SINGLE_USER WITH ROLLBACK IMMEDIATE). The
// database returns to MULTI_USER even when the restore fails.
func RevertToBaseline(ctx context.Context, opts Opts, db string, out io.Writer) error {
	if !safeIdent.MatchString(db) {
		return fmt.Errorf("invalid database name %q", db)
	}
	exists, err := BaselineExists(ctx, opts, db)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w for %q — rerun `orbit sqlserver reset %s` to rebuild a clean state", ErrBaselineMissing, db, db)
	}

	pool, err := openMasterDB(opts)
	if err != nil {
		return err
	}
	defer func() { _ = pool.Close() }()

	// Pin ONE session for the whole single-user window: sql.DB is a
	// pool, and separate ExecContext calls may run on different
	// connections — the gap would let another client claim the
	// database's single-user slot between ALTER and RESTORE.
	sess, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()

	snap := BaselineName(db)
	fmt.Fprintf(out, "[reset] disconnecting clients of %s\n", db)
	if _, err := sess.ExecContext(ctx, "ALTER DATABASE ["+db+"] SET SINGLE_USER WITH ROLLBACK IMMEDIATE"); err != nil {
		return fmt.Errorf("taking %s single-user: %w", db, err)
	}
	fmt.Fprintf(out, "[reset] restoring the clean state for %s\n", db)
	_, restoreErr := sess.ExecContext(ctx, fmt.Sprintf("RESTORE DATABASE [%s] FROM DATABASE_SNAPSHOT = '%s'", db, snap))

	// MULTI_USER must be attempted even when the operation context is
	// dead (timeout/cancel is exactly when we'd strand SINGLE_USER) —
	// bounded independent context, fresh connection as fallback.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, multiErr := sess.ExecContext(cleanupCtx, "ALTER DATABASE ["+db+"] SET MULTI_USER")
	if multiErr != nil {
		if _, retryErr := pool.ExecContext(cleanupCtx, "ALTER DATABASE ["+db+"] SET MULTI_USER"); retryErr == nil {
			multiErr = nil
		}
	}

	switch {
	case restoreErr != nil && multiErr != nil:
		return fmt.Errorf("restore failed AND the database may be stuck in SINGLE_USER — recover manually with `ALTER DATABASE [%s] SET MULTI_USER`: %w", db, errors.Join(restoreErr, multiErr))
	case restoreErr != nil:
		return fmt.Errorf("restoring the clean state: %w", restoreErr)
	case multiErr != nil:
		return fmt.Errorf("restore succeeded but the database may be stuck in SINGLE_USER — recover manually with `ALTER DATABASE [%s] SET MULTI_USER`: %w", db, multiErr)
	}
	fmt.Fprintf(out, "[reset] %s restored\n", db)
	return nil
}

// PublishClean is the clean publish: BUILD first (zero side effects —
// a broken project costs nothing), then revert to baseline, publish
// the built dacpac, refresh the baseline (the one moment the data is
// known clean). Requires an existing baseline.
func PublishClean(ctx context.Context, opts Opts, out io.Writer) Result {
	start := time.Now()

	// The build can fail without touching the database, so it goes
	// first; RevertToBaseline then verifies the baseline exists before
	// its own destructive steps.
	dacpac, fingerprint, code, err := buildDacpac(ctx, opts, out)
	if err != nil {
		return failed(start, err, code)
	}

	if err := RevertToBaseline(ctx, opts, opts.DB, out); err != nil {
		if errors.Is(err, ErrBaselineMissing) {
			return failed(start, err, CodeCleanStateMissing)
		}
		return failed(start, err, CodeResetRestoreFailed)
	}

	// A clean publish deliberately discards data — the revert above already
	// threw away everything since the baseline. So the schema publish that
	// follows MUST allow data loss: when the latest schema makes a
	// destructive change to a table the baseline populated (e.g. dropping a
	// column on a table with rows), the default BlockOnPossibleDataLoss=true
	// would abort here, stranding the DB half-reset (data gone, schema not
	// advanced, baseline not refreshed). Force it regardless of what the
	// caller passed — the reset/clean contract owns this, not the caller.
	opts.Force = true

	if code, err := publishWithCompositeRetry(ctx, opts, dacpac, out); err != nil {
		// The revert already happened: data changes since the baseline are
		// gone, the schema publish failed, the baseline is deliberately
		// untouched. An ordinary publish-failure message would hide the
		// destructive half — say all three things.
		_ = code // the partial state dominates whatever the publish-stage class was
		return failed(start,
			fmt.Errorf("reset partial failure: local data was discarded, then the schema update failed; fix the publish error and run reset again: %w", err),
			CodeResetPartial)
	}

	if err := RefreshBaseline(ctx, opts, opts.DB, out); err != nil {
		return failed(start, err, CodeResetPrepareFailed)
	}
	// Remember what was just published so the next diff can short-circuit.
	recordPublishStateWhenAvailable(ctx, opts, fingerprint, out)
	return Result{OK: true, DurationMs: time.Since(start).Milliseconds()}
}

// DropDatabase force-disconnects clients and drops the database. Reset's
// from-scratch path uses it for a legacy DB with no baseline — the only
// route back to a known-clean state is to rebuild it. Destructive: every
// row is gone. A missing database is a no-op (already absent), so a
// rerun after a partial drop still converges.
func DropDatabase(ctx context.Context, opts Opts, db string, out io.Writer) error {
	if !safeIdent.MatchString(db) {
		return fmt.Errorf("invalid database name %q", db)
	}
	conn, err := openMasterDB(opts)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	fmt.Fprintf(out, "[reset] dropping %s\n", db)
	// A source database can't be dropped while snapshots of it exist, so
	// clear any first (a stale baseline/temp RefreshBaseline may have
	// left). sys.databases is trusted metadata, but the name is still
	// validated before it goes into DDL.
	snaps, err := snapshotsOf(ctx, conn, db)
	if err != nil {
		return fmt.Errorf("listing snapshots of %s: %w", db, err)
	}
	for _, snap := range snaps {
		if !safeIdent.MatchString(snap) {
			continue
		}
		if _, err := conn.ExecContext(ctx, "DROP DATABASE IF EXISTS ["+snap+"]"); err != nil {
			return fmt.Errorf("dropping snapshot %s of %s: %w", snap, db, err)
		}
	}

	// Pin ONE session for the SINGLE_USER→DROP window (same reason as
	// RevertToBaseline): sql.DB is a pool, and a gap between the two
	// statements on different connections would let another client claim
	// the single-user slot and block the DROP.
	sess, err := conn.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()

	// SINGLE_USER WITH ROLLBACK IMMEDIATE evicts active sessions so the
	// DROP can't be blocked; the DB_ID guard skips it when the database
	// is already gone (SINGLE_USER on a missing DB would error).
	if _, err := sess.ExecContext(ctx, "IF DB_ID('"+db+"') IS NOT NULL ALTER DATABASE ["+db+"] SET SINGLE_USER WITH ROLLBACK IMMEDIATE"); err != nil {
		return fmt.Errorf("disconnecting clients of %s: %w", db, err)
	}
	if _, err := sess.ExecContext(ctx, "DROP DATABASE IF EXISTS ["+db+"]"); err != nil {
		// The DB is now SINGLE_USER but not dropped — every client is
		// locked out. Restore MULTI_USER so a failed drop isn't a team
		// outage; use a fresh bounded context because a dead ctx
		// (timeout/cancel) is exactly when we'd strand it.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, muErr := sess.ExecContext(cleanupCtx, "IF DB_ID('"+db+"') IS NOT NULL ALTER DATABASE ["+db+"] SET MULTI_USER"); muErr != nil {
			return fmt.Errorf("dropping %s failed AND it may be stuck in SINGLE_USER — recover manually with `ALTER DATABASE [%s] SET MULTI_USER`: %w", db, db, errors.Join(err, muErr))
		}
		return fmt.Errorf("dropping %s: %w", db, err)
	}
	return nil
}

// snapshotsOf returns the names of database snapshots whose source is db.
func snapshotsOf(ctx context.Context, conn *sql.DB, db string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, "SELECT name FROM sys.databases WHERE source_database_id = DB_ID(@p1)", db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// Reset returns a database to a clean state at the latest schema,
// discarding local data changes — the user-facing "reset" verb.
//
// With a baseline it is a clean publish: revert → publish → refresh
// baseline. Without one — a legacy DB that predates baselining or came
// from a retired flow — the only route to clean is a destructive
// from-scratch redeploy: drop the DB, publish (which recreates it clean
// via composite), and declare its first baseline. That path runs ONLY
// when allowRecreate is set; otherwise Reset returns CodeCleanStateMissing
// unchanged. This is a safety gate, not just UX: the baseline is
// re-checked here (not at the caller's earlier probe), so a baseline
// that vanished in between never silently escalates a plain reset into a
// full rebuild. Callers confirm the destructive intent before setting
// allowRecreate — Reset does not prompt.
func Reset(ctx context.Context, opts Opts, allowRecreate bool, out io.Writer) Result {
	res := PublishClean(ctx, opts, out)
	if res.Code != CodeCleanStateMissing {
		return res
	}
	if !allowRecreate {
		// No baseline and no authorization to rebuild — refuse rather
		// than dropping the database the caller didn't agree to lose.
		return res
	}

	// Rebuild from scratch: the only honest way to reach a known-clean
	// state when no clean point was ever recorded.
	start := time.Now()
	fmt.Fprintf(out, "[reset] preparing %s from scratch (all current data is discarded)\n", opts.DB)
	if err := DropDatabase(ctx, opts, opts.DB, out); err != nil {
		return failed(start, err, CodePublishFailed)
	}
	if pub := Publish(ctx, opts, out); !pub.OK {
		return pub
	}
	if err := RefreshBaseline(ctx, opts, opts.DB, out); err != nil {
		return failed(start, err, CodeResetPrepareFailed)
	}
	return Result{OK: true, DurationMs: time.Since(start).Milliseconds(), Created: true}
}
