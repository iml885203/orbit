package sqlpublish

// Live-server e2e for the quick diff path. Skipped unless
// ORBIT_E2E_SQL is set to a reachable SQL Server as "host:port:sapassword"
// (e.g. "localhost:11433:secret"). It creates and drops a throwaway
// database named OrbitQuickDiffE2E and isolates all caches under a temp
// ORBIT_HOME — the host's real orbit state is never touched.
//
//	ORBIT_E2E_SQL="localhost:11433:$SA_PASSWORD" go test ./internal/sqlpublish/ -run TestE2E_QuickDiff -v

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func e2eOpts(t *testing.T) Opts {
	t.Helper()
	spec := os.Getenv("ORBIT_E2E_SQL")
	if spec == "" {
		t.Skip("ORBIT_E2E_SQL not set — live SQL Server e2e skipped")
	}
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) != 3 {
		t.Fatalf("ORBIT_E2E_SQL must be host:port:sapassword, got %q", spec)
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("bad port in ORBIT_E2E_SQL: %v", err)
	}
	t.Setenv("ORBIT_HOME", t.TempDir())
	return Opts{
		DB:       "OrbitQuickDiffE2E",
		Host:     parts[0],
		Port:     port,
		User:     "sa",
		Password: parts[2],
	}
}

// writeE2EProj lays down a small but real SSDT project (SDK build).
//
// The project filename deliberately differs from the target database name:
// while they matched, this test — the live-SQL gate a release runs — could
// not have caught a publish path that derived build artifacts from the
// database name instead of the project's.
func writeE2EProj(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	proj := filepath.Join(dir, "OrbitQuickDiffProject.sqlproj")
	sqlproj := `<Project DefaultTargets="Build" xmlns="http://schemas.microsoft.com/developer/msbuild/2003" ToolsVersion="4.0">
  <Sdk Name="Microsoft.Build.Sql" Version="2.1.0" />
  <PropertyGroup>
    <Name>OrbitQuickDiffProject</Name>
    <DSP>Microsoft.Data.Tools.Schema.Sql.Sql160DatabaseSchemaProvider</DSP>
    <ModelCollation>1033, CI</ModelCollation>
  </PropertyGroup>
</Project>`
	if err := os.WriteFile(proj, []byte(sqlproj), 0o644); err != nil {
		t.Fatal(err)
	}
	table := "CREATE TABLE [dbo].[Customer] ([Id] INT NOT NULL PRIMARY KEY, [Name] NVARCHAR(100) NOT NULL);"
	if err := os.WriteFile(filepath.Join(dir, "Customer.sql"), []byte(table), 0o644); err != nil {
		t.Fatal(err)
	}
	return proj
}

func TestE2E_QuickDiff(t *testing.T) {
	opts := e2eOpts(t)
	opts.SQLProj = writeE2EProj(t)
	opts.OutDir = t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := DropDatabase(cleanupCtx, opts, opts.DB, os.Stderr); err != nil {
			t.Errorf("cleanup: dropping %s: %v", opts.DB, err)
		}
	})
	start := time.Now()
	if res := Publish(ctx, opts, os.Stderr); !res.OK {
		t.Fatalf("publish failed: %v", res.Err)
	}
	t.Logf("publish: %v", time.Since(start).Round(time.Millisecond))

	t.Run("state guards", func(t *testing.T) { testE2EStateGuards(t, ctx, opts) })
	t.Run("quick in sync", func(t *testing.T) { testE2EQuickInSync(t, ctx, opts) })
	t.Run("procedure add and replay", func(t *testing.T) { testE2EProcedureAddReplay(t, ctx, opts) })
	t.Run("procedure deletion", func(t *testing.T) { testE2EProcedureDeletion(t, ctx, opts) })
	t.Run("table edit", func(t *testing.T) { testE2ETableEdit(t, ctx, opts) })
	t.Run("external drift", func(t *testing.T) { testE2EExternalDrift(t, ctx, opts) })
	t.Run("baseline reset", func(t *testing.T) { testE2EBaselineReset(t, ctx, opts) })
}

func testE2EStateGuards(t *testing.T, ctx context.Context, opts Opts) {
	t.Helper()
	publishedFingerprint, err := projectFingerprint(opts.SQLProj, opts.DB)
	if err != nil {
		t.Fatal(err)
	}
	publishedMarkers, err := dbMarkers(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	raceFile := filepath.Join(filepath.Dir(opts.SQLProj), "Race.sql")
	if err := os.WriteFile(raceFile, []byte("CREATE PROCEDURE [dbo].[Race] AS SELECT 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recordPublishState(ctx, opts, publishedFingerprint); err == nil {
		t.Error("publish state accepted a fingerprint from before a source edit")
	}
	if err := recordDiffCache(ctx, opts, publishedFingerprint, publishedMarkers, DiffResult{DB: opts.DB, InSync: true}); err == nil {
		t.Error("diff cache accepted a result from before a source edit")
	}
	if err := os.Remove(raceFile); err != nil {
		t.Fatal(err)
	}
	execTarget(t, opts, "CREATE PROCEDURE [dbo].[OrbitExternalDrift] AS SELECT 1")
	if err := recordDiffCache(ctx, opts, publishedFingerprint, publishedMarkers, DiffResult{DB: opts.DB, InSync: true}); err == nil {
		t.Error("diff cache accepted a result from before database drift")
	}
	execTarget(t, opts, "DROP PROCEDURE [dbo].[OrbitExternalDrift]")
}

func testE2EQuickInSync(t *testing.T, ctx context.Context, opts Opts) {
	t.Helper()
	start := time.Now()
	res, code, err := Diff(ctx, opts, os.Stderr)
	quickDur := time.Since(start)
	if err != nil {
		t.Fatalf("quick diff failed (%s): %v", code, err)
	}
	t.Logf("quick diff: %v (quick=%v in_sync=%v)", quickDur.Round(time.Millisecond), res.Quick, res.InSync)
	if !res.Quick || !res.InSync {
		t.Errorf("diff right after publish must be quick+in-sync, got quick=%v in_sync=%v", res.Quick, res.InSync)
	}
	if quickDur > 500*time.Millisecond {
		t.Errorf("quick diff took %v; the target is <500ms", quickDur)
	}
	analyzed := opts
	analyzed.Analyze = true
	analyzed.OutDir = t.TempDir()
	start = time.Now()
	res, code, err = Diff(ctx, analyzed, os.Stderr)
	if err != nil {
		t.Fatalf("analyzed diff failed (%s): %v", code, err)
	}
	t.Logf("analyzed diff: %v (quick=%v in_sync=%v)", time.Since(start).Round(time.Millisecond), res.Quick, res.InSync)
	if res.Quick {
		t.Error("analyzed diff must not use the quick path")
	}
}

func testE2EProcedureAddReplay(t *testing.T, ctx context.Context, opts Opts) {
	t.Helper()
	proc := "CREATE PROCEDURE [dbo].[GetCustomer] @Id INT AS SELECT Id, Name FROM dbo.Customer WHERE Id = @Id;"
	if err := os.WriteFile(filepath.Join(filepath.Dir(opts.SQLProj), "GetCustomer.sql"), []byte(proc), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := opts
	changed.OutDir = t.TempDir()
	start := time.Now()
	res, code, err := Diff(ctx, changed, os.Stderr)
	fileDur := time.Since(start)
	if err != nil {
		t.Fatalf("post-edit diff failed (%s): %v", code, err)
	}
	t.Logf("post-edit diff: %v (quick=%v file_changes=%v)", fileDur.Round(time.Millisecond), res.Quick, res.FileChanges)
	if !res.Quick || len(res.FileChanges) != 1 ||
		res.FileChanges[0].Action != "Added" || filepath.Base(res.FileChanges[0].Path) != "GetCustomer.sql" {
		t.Errorf("post-edit diff must name the added file, got %+v", res)
	}
	if fileDur > 500*time.Millisecond {
		t.Errorf("file-level diff took %v; the target is <500ms", fileDur)
	}
	postEditAnalyzed := changed
	postEditAnalyzed.Analyze = true
	postEditAnalyzed.OutDir = t.TempDir()
	start = time.Now()
	res, code, err = Diff(ctx, postEditAnalyzed, os.Stderr)
	if err != nil {
		t.Fatalf("post-edit analyzed diff failed (%s): %v", code, err)
	}
	t.Logf("post-edit analyzed diff: %v (created=%d)", time.Since(start).Round(time.Millisecond), res.Created)
	if res.InSync || res.Created != 1 {
		t.Errorf("post-edit analyzed diff must report the new procedure, got in_sync=%v created=%d", res.InSync, res.Created)
	}
	testE2ECachedReplay(t, ctx, changed)
}

func testE2ECachedReplay(t *testing.T, ctx context.Context, replay Opts) {
	t.Helper()
	replay.OutDir = t.TempDir()
	start := time.Now()
	res, code, err := Diff(ctx, replay, os.Stderr)
	replayDur := time.Since(start)
	if err != nil {
		t.Fatalf("replay diff failed (%s): %v", code, err)
	}
	t.Logf("replay diff: %v (cached=%v created=%d)", replayDur.Round(time.Millisecond), res.Cached, res.Created)
	if !res.Cached || res.Created != 1 || len(res.Ops) != 1 {
		t.Errorf("replay diff must return the cached engine ops, got %+v", res)
	}
	if replayDur > 500*time.Millisecond {
		t.Errorf("cached replay took %v; the target is <500ms", replayDur)
	}
}

func testE2EProcedureDeletion(t *testing.T, ctx context.Context, opts Opts) {
	t.Helper()
	published := opts
	published.OutDir = t.TempDir()
	if publishResult := Publish(ctx, published, os.Stderr); !publishResult.OK {
		t.Fatalf("procedure publish failed: %v", publishResult.Err)
	}
	if err := os.Remove(filepath.Join(filepath.Dir(opts.SQLProj), "GetCustomer.sql")); err != nil {
		t.Fatal(err)
	}
	deleted := opts
	deleted.Analyze = true
	deleted.OutDir = t.TempDir()
	res, code, err := Diff(ctx, deleted, os.Stderr)
	if err != nil {
		t.Fatalf("post-delete analyzed diff failed (%s): %v", code, err)
	}
	if res.InSync || res.Dropped != 1 {
		t.Errorf("engine must report the target-only procedure as a drop, got %+v", res)
	}

	deleted.Analyze = false
	deleted.OutDir = t.TempDir()
	res, code, err = Diff(ctx, deleted, os.Stderr)
	if err != nil {
		t.Fatalf("post-delete replay failed (%s): %v", code, err)
	}
	if !res.Cached || res.InSync || res.Dropped != 1 || len(res.FileChanges) != 1 ||
		res.FileChanges[0].Action != "Deleted" || filepath.Base(res.FileChanges[0].Path) != "GetCustomer.sql" {
		t.Errorf("cached engine result must preserve the source deletion, got %+v", res)
	}

	deleted.OutDir = t.TempDir()
	if publishResult := Publish(ctx, deleted, os.Stderr); !publishResult.OK {
		t.Fatalf("procedure deletion publish failed: %v", publishResult.Err)
	}
	if objectExists(t, opts, "dbo.GetCustomer") {
		t.Error("procedure survived publishing its source deletion")
	}
}

func testE2ETableEdit(t *testing.T, ctx context.Context, opts Opts) {
	t.Helper()
	customerFile := filepath.Join(filepath.Dir(opts.SQLProj), "Customer.sql")
	customerWithEmail := "CREATE TABLE [dbo].[Customer] ([Id] INT NOT NULL PRIMARY KEY, [Name] NVARCHAR(100) NOT NULL, [Email] NVARCHAR(200) NULL);"
	if err := os.WriteFile(customerFile, []byte(customerWithEmail), 0o644); err != nil {
		t.Fatal(err)
	}
	tableEdit := opts
	tableEdit.OutDir = t.TempDir()
	res, code, err := Diff(ctx, tableEdit, os.Stderr)
	if err != nil {
		t.Fatalf("table file diff failed (%s): %v", code, err)
	}
	if !res.Quick || len(res.FileChanges) != 1 || res.FileChanges[0].Action != "Modified" {
		t.Errorf("table edit must be a quick modified-file result, got %+v", res)
	}
	tableEdit.Analyze = true
	tableEdit.OutDir = t.TempDir()
	res, code, err = Diff(ctx, tableEdit, os.Stderr)
	if err != nil {
		t.Fatalf("table analysis failed (%s): %v", code, err)
	}
	if res.Altered == 0 {
		t.Errorf("adding a column must alter the table, got %+v", res)
	}
	tableEdit.Analyze = false
	tableEdit.OutDir = t.TempDir()
	if publishResult := Publish(ctx, tableEdit, os.Stderr); !publishResult.OK {
		t.Fatalf("table publish failed: %v", publishResult.Err)
	}
	res, code, err = Diff(ctx, tableEdit, os.Stderr)
	if err != nil || !res.Quick || !res.InSync {
		t.Errorf("published table must return to quick in-sync: result=%+v code=%s err=%v", res, code, err)
	}
}

func testE2EExternalDrift(t *testing.T, ctx context.Context, opts Opts) {
	t.Helper()
	execTarget(t, opts, "INSERT INTO [dbo].[Customer] ([Id], [Name]) VALUES (99, 'drift')")
	execTarget(t, opts, "ALTER TABLE [dbo].[Customer] ADD [ExternalOnly] INT NULL")
	execTarget(t, opts, "UPDATE [dbo].[Customer] SET [ExternalOnly] = 1 WHERE [Id] = 99")
	drifted := opts
	drifted.Analyze = true
	drifted.OutDir = t.TempDir()
	res, code, err := Diff(ctx, drifted, os.Stderr)
	if err != nil {
		t.Fatalf("external drift analysis failed (%s): %v", code, err)
	}
	if res.InSync || res.Altered == 0 {
		t.Errorf("external table drift was not detected: %+v", res)
	}
	drifted.Analyze = false
	drifted.OutDir = t.TempDir()
	if publishResult := Publish(ctx, drifted, os.Stderr); publishResult.OK || publishResult.Code != CodePublishBlockedDataLoss {
		t.Fatalf("destructive drift publish was not blocked: OK=%v code=%s err=%v",
			publishResult.OK, publishResult.Code, publishResult.Err)
	}
	drifted.Force = true
	drifted.OutDir = t.TempDir()
	if publishResult := Publish(ctx, drifted, os.Stderr); !publishResult.OK {
		t.Fatalf("restoring external drift failed: %v", publishResult.Err)
	}
	if columnExists(t, opts, "[dbo].[Customer]", "ExternalOnly") {
		t.Error("force publish left the external-only column behind")
	}
	execTarget(t, opts, "DELETE FROM [dbo].[Customer] WHERE [Id] = 99")
}

func testE2EBaselineReset(t *testing.T, ctx context.Context, opts Opts) {
	t.Helper()
	if err := RefreshBaseline(ctx, opts, opts.DB, os.Stderr); err != nil {
		t.Fatalf("refresh baseline: %v", err)
	}
	execTarget(t, opts, "INSERT INTO [dbo].[Customer] ([Id], [Name]) VALUES (1, 'local')")
	resetOpts := opts
	resetOpts.OutDir = t.TempDir()
	if resetResult := Reset(ctx, resetOpts, false, os.Stderr); !resetResult.OK {
		t.Fatalf("baseline reset failed: %v (code=%s)", resetResult.Err, resetResult.Code)
	}
	db := targetConn(t, opts)
	var customerCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM [dbo].[Customer]").Scan(&customerCount); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()
	if customerCount != 0 {
		t.Errorf("reset retained %d local customer rows", customerCount)
	}
}

// TestE2E_FingerprintCostOnRealProject prices the quick path's file walk
// on a production-sized project (set ORBIT_E2E_SQLPROJ to a real
// .sqlproj) — the walk plus two catalog queries is the entire quick-path
// budget, so this is the number that must stay well under 0.5s.
func TestE2E_FingerprintCostOnRealProject(t *testing.T) {
	proj := os.Getenv("ORBIT_E2E_SQLPROJ")
	if proj == "" {
		t.Skip("ORBIT_E2E_SQLPROJ not set")
	}
	// Warm-up then measure: steady-state is what a repeated badge
	// refresh sees; the cold first walk is logged for honesty.
	start := time.Now()
	if _, err := projectFingerprint(proj, "TimingProbe"); err != nil {
		t.Fatal(err)
	}
	cold := time.Since(start)
	start = time.Now()
	if _, err := projectFingerprint(proj, "TimingProbe"); err != nil {
		t.Fatal(err)
	}
	warm := time.Since(start)
	fmt.Printf("projectFingerprint on %s: cold=%v warm=%v\n", filepath.Base(proj), cold.Round(time.Millisecond), warm.Round(time.Millisecond))
	if warm > 400*time.Millisecond {
		t.Errorf("warm fingerprint walk took %v; leaves no budget for the catalog queries", warm)
	}
}
