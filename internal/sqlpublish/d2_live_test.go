package sqlpublish

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestD2LiveReset exercises both reset paths against a real, empty SQL
// Server. Shares the ORBIT_D1_LIVE guard and env with TestD1Live; it is
// self-contained (drops the target first) so it can run in the same
// throwaway container regardless of order.
func TestD2LiveReset(t *testing.T) {
	if os.Getenv("ORBIT_D1_LIVE") == "" {
		t.Skip("set ORBIT_D1_LIVE to run the live D2 verification")
	}
	port, err := strconv.Atoi(os.Getenv("ORBIT_D1_PORT"))
	if err != nil {
		t.Fatalf("ORBIT_D1_PORT: %v", err)
	}
	opts := Opts{
		DB:       os.Getenv("ORBIT_D1_DB"),
		SQLProj:  os.Getenv("ORBIT_D1_PROJ"),
		OutDir:   t.TempDir(),
		Host:     "localhost",
		Port:     port,
		User:     "sa",
		Password: os.Getenv("ORBIT_D1_PW"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Clean slate: drop the DB and its baseline so this test is order-free.
	if err := DropDatabase(ctx, opts, opts.DB, os.Stdout); err != nil {
		t.Fatalf("pre-drop: %v", err)
	}

	// Create the DB without a baseline (engine Publish never baselines) —
	// the legacy shape reset's from-scratch path must handle.
	if res := Publish(ctx, opts, os.Stdout); !res.OK {
		t.Fatalf("seed publish: %v (code=%s)", res.Err, res.Code)
	}
	if has, err := BaselineExists(ctx, opts, opts.DB); err != nil || has {
		t.Fatalf("BaselineExists after plain publish = %v, err=%v; want false", has, err)
	}

	// Safety gate: without allowRecreate, a no-baseline reset must REFUSE
	// (CodeCleanStateMissing), never silently drop-and-rebuild.
	if res := Reset(ctx, opts, false, os.Stdout); res.OK || res.Code != CodeCleanStateMissing {
		t.Fatalf("reset (no baseline, allowRecreate=false) = OK:%v code:%s; want refusal with CodeCleanStateMissing", res.OK, res.Code)
	}

	// With allowRecreate → from-scratch rebuild → baseline now exists.
	if res := Reset(ctx, opts, true, os.Stdout); !res.OK {
		t.Fatalf("reset (no baseline, allowRecreate=true): %v (code=%s)", res.Err, res.Code)
	}
	if has, err := BaselineExists(ctx, opts, opts.DB); err != nil || !has {
		t.Fatalf("BaselineExists after from-scratch reset = %v, err=%v; want true", has, err)
	}

	// Write post-baseline data, then reset WITH a baseline → clean publish
	// path reverts it away (allowRecreate irrelevant — baseline exists).
	execTarget(t, opts, "CREATE TABLE dbo.ResetProbe (id INT)")
	if res := Reset(ctx, opts, false, os.Stdout); !res.OK {
		t.Fatalf("reset (with baseline): %v (code=%s)", res.Err, res.Code)
	}
	if objectExists(t, opts, "dbo.ResetProbe") {
		t.Errorf("dbo.ResetProbe survived reset — revert to baseline did not discard post-baseline changes")
	}

	// Regression for the data-loss reset bug: the baseline must carry DATA
	// on a table the project defines, plus a schema change the publish
	// makes DESTRUCTIVELY. We add a column the project's schema does NOT
	// have to an existing project table, populate it, and fold that into
	// the baseline. On reset, revert restores the extra column + its data,
	// then the project publish wants to DROP that column — a data-loss
	// operation on a populated table.
	//
	// A column on a project-owned table keeps this regression focused on the
	// baseline schema transition rather than target-only object cleanup.
	//
	// Before the fix the publish ran with BlockOnPossibleDataLoss=true and
	// aborted here, stranding the DB half-reset (data gone, schema not
	// advanced, baseline not refreshed). PublishClean now forces data loss.
	probeTable := firstUserTable(t, opts)
	execTarget(t, opts, "ALTER TABLE "+probeTable+" ADD orbit_reset_probe_col NVARCHAR(16) NULL")
	execTarget(t, opts, "UPDATE "+probeTable+" SET orbit_reset_probe_col = 'x'")
	if err := RefreshBaseline(ctx, opts, opts.DB, os.Stdout); err != nil {
		t.Fatalf("refresh baseline with an extra populated column: %v", err)
	}
	if res := Reset(ctx, opts, false, os.Stdout); !res.OK {
		t.Fatalf("reset (baseline column has data + destructive publish) failed — the data-loss bug is back: %v (code=%s)", res.Err, res.Code)
	}
	if columnExists(t, opts, probeTable, "orbit_reset_probe_col") {
		t.Errorf("orbit_reset_probe_col survived reset — publish did not drop the populated column the project omits")
	}
	// Baseline must have been refreshed to the clean (project-only) schema:
	// a follow-up reset stays green rather than re-encountering the drop.
	if res := Reset(ctx, opts, false, os.Stdout); !res.OK {
		t.Fatalf("second reset after data-loss reset failed — baseline was not refreshed clean: %v (code=%s)", res.Err, res.Code)
	}
}

// firstUserTable returns a schema-qualified user table name from the
// target DB, for tests that need an existing project table to mutate.
func firstUserTable(t *testing.T, opts Opts) string {
	t.Helper()
	db := targetConn(t, opts)
	defer func() { _ = db.Close() }()
	var name string
	err := db.QueryRow(
		"SELECT TOP 1 QUOTENAME(SCHEMA_NAME(schema_id)) + '.' + QUOTENAME(name) " +
			"FROM sys.tables WHERE is_ms_shipped = 0 ORDER BY name").Scan(&name)
	if err != nil {
		t.Fatalf("find a user table: %v", err)
	}
	return name
}

func columnExists(t *testing.T, opts Opts, table, column string) bool {
	t.Helper()
	db := targetConn(t, opts)
	defer func() { _ = db.Close() }()
	var n sql.NullInt64
	if err := db.QueryRow("SELECT COL_LENGTH(@p1, @p2)", table, column).Scan(&n); err != nil {
		t.Fatalf("COL_LENGTH(%q,%q): %v", table, column, err)
	}
	return n.Valid // NULL → column absent
}

// targetConn opens a fresh pool to the target database (not master) — a
// new pool each call so it never reuses a connection killed by a reset.
func targetConn(t *testing.T, opts Opts) *sql.DB {
	t.Helper()
	u := url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(opts.User, opts.Password),
		Host:     net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)),
		RawQuery: url.Values{"database": {opts.DB}, "TrustServerCertificate": {"true"}}.Encode(),
	}
	db, err := sql.Open("sqlserver", u.String())
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	return db
}

func execTarget(t *testing.T, opts Opts, stmt string) {
	t.Helper()
	db := targetConn(t, opts)
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("exec %q: %v", stmt, err)
	}
}

func objectExists(t *testing.T, opts Opts, name string) bool {
	t.Helper()
	db := targetConn(t, opts)
	defer func() { _ = db.Close() }()
	var id sql.NullInt64
	if err := db.QueryRow("SELECT OBJECT_ID(@p1)", name).Scan(&id); err != nil {
		t.Fatalf("OBJECT_ID(%q): %v", name, err)
	}
	return id.Valid
}
