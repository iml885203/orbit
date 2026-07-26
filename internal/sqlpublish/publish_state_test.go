package sqlpublish

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// statePublishOpts returns Opts pointing a toy project at a port nothing
// listens on — fast-path tests must fail open (fall through to the
// engine) rather than ever reaching a real server.
func statePublishOpts(t *testing.T, proj string) Opts {
	t.Helper()
	t.Setenv("ORBIT_HOME", t.TempDir())
	return Opts{
		DB:       "AppDB",
		SQLProj:  proj,
		Host:     "127.0.0.1",
		Port:     1, // nothing listens here; connections are refused immediately
		User:     "sa",
		Password: "x",
	}
}

func TestPublishState_RoundTrip(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	want := publishState{
		recordedState: recordedState{
			Host: "localhost", Port: 14330, TargetID: "env-a/image:v1",
			Fingerprint: "abc123",
			dbMarkerSet: dbMarkerSet{
				DBCreateDate: "2026-07-24T00:00:00", ObjectCount: 42, MaxModifyDate: "2026-07-24T01:00:00",
			},
			At: time.Now().Format(time.RFC3339),
		},
		Files: map[string]fileState{
			"/proj/Table.sql": {Size: 10, MtimeNs: 999, SHA: "deadbeef"},
		},
	}
	if err := writeStateFile(publishStateDir, "AppDB", want); err != nil {
		t.Fatal(err)
	}
	got, ok := loadPublishState("AppDB")
	if !ok {
		t.Fatal("record just written must load")
	}
	if got.TargetID != want.TargetID || got.Fingerprint != want.Fingerprint || got.dbMarkerSet != want.dbMarkerSet ||
		len(got.Files) != 1 || got.Files["/proj/Table.sql"] != want.Files["/proj/Table.sql"] {
		t.Errorf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestDiffCache_RoundTrip(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	want := diffCacheEntry{
		recordedState: recordedState{
			Host: "localhost", Port: 14330, TargetID: "env-a/image:v1",
			Fingerprint: "abc123",
			dbMarkerSet: dbMarkerSet{DBCreateDate: "2026-07-24T00:00:00", ObjectCount: 7, MaxModifyDate: "2026-07-24T01:00:00"},
			At:          time.Now().Format(time.RFC3339),
		},
		Result: DiffResult{
			DB: "AppDB", Created: 1,
			Ops: []DiffOp{{Action: "Create", ObjectType: "SqlProcedure", Name: "[dbo].[GetX]"}},
		},
	}
	if err := writeStateFile(diffResultsDir, "AppDB", want); err != nil {
		t.Fatal(err)
	}
	got, ok := loadDiffCache("AppDB")
	if !ok {
		t.Fatal("cache entry just written must load")
	}
	if got.TargetID != want.TargetID || got.Fingerprint != want.Fingerprint || got.dbMarkerSet != want.dbMarkerSet ||
		got.Result.Created != 1 || len(got.Result.Ops) != 1 || got.Result.Ops[0] != want.Result.Ops[0] {
		t.Errorf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestReplayCachedDiff_PreservesSourceDeletionWhenEngineHasNoOperations(t *testing.T) {
	result := replayCachedDiff(
		DiffResult{DB: "AppDB", InSync: true},
		[]FileChange{{Action: "Deleted", Path: "AppDB/dbo/Procedures/Gone.sql"}},
	)

	if !result.Cached {
		t.Error("replayed engine result must be marked cached")
	}
	if result.InSync {
		t.Error("a source deletion must not be hidden by an engine no-op")
	}
	if len(result.FileChanges) != 1 || result.FileChanges[0].Action != "Deleted" {
		t.Fatalf("source deletion missing from replay: %+v", result)
	}
}

func TestReplayCachedDiff_WithoutSourceChangesKeepsEngineResult(t *testing.T) {
	result := replayCachedDiff(DiffResult{
		DB:      "AppDB",
		Created: 1,
		Ops:     []DiffOp{{Action: "Create", ObjectType: "SqlProcedure", Name: "[dbo].[GetX]"}},
	}, nil)

	if !result.Cached || result.Created != 1 || len(result.Ops) != 1 {
		t.Fatalf("cached engine result changed unexpectedly: %+v", result)
	}
	if len(result.FileChanges) != 0 {
		t.Fatalf("unexpected source changes: %+v", result.FileChanges)
	}
}

func TestLoadPublishState_MissingRecord(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	if _, ok := loadPublishState("NeverPublished"); ok {
		t.Error("missing record must report no usable record")
	}
}

func TestFastDiff_NoRecordFallsThrough(t *testing.T) {
	proj := writeProj(t, "CREATE TABLE X (id INT)")
	opts := statePublishOpts(t, proj)
	if _, ok := FastDiff(context.Background(), opts, io.Discard); ok {
		t.Error("no record must fall through to the engine diff")
	}
}

func TestFastDiff_TargetMismatchFallsThrough(t *testing.T) {
	proj := writeProj(t, "CREATE TABLE X (id INT)")
	opts := statePublishOpts(t, proj)
	fp, err := projectFingerprint(proj, opts.DB)
	if err != nil {
		t.Fatal(err)
	}
	// Same fingerprint but recorded against a different server target: the
	// record says nothing about THIS database.
	if err := writeStateFile(publishStateDir, opts.DB, publishState{
		recordedState: recordedState{Host: "otherhost", Port: 9999, Fingerprint: fp},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := FastDiff(context.Background(), opts, io.Discard); ok {
		t.Error("record for a different host:port must fall through")
	}
}

func TestFastDiff_EnvOrImageMismatchFallsThrough(t *testing.T) {
	proj := writeProj(t, "CREATE TABLE X (id INT)")
	opts := statePublishOpts(t, proj)
	opts.TargetID = "env-b/image:v2"
	fp, err := projectFingerprint(proj, opts.DB)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeStateFile(publishStateDir, opts.DB, publishState{
		recordedState: recordedState{
			Host: opts.Host, Port: opts.Port, TargetID: "env-a/image:v1", Fingerprint: fp,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := FastDiff(context.Background(), opts, io.Discard); ok {
		t.Error("record from another env/image must never answer this target")
	}
}

func TestFastDiff_EngineCacheDoesNotCrossEnvOrImage(t *testing.T) {
	proj := writeProj(t, "CREATE TABLE X (id INT)")
	opts := statePublishOpts(t, proj)
	opts.TargetID = "env-b/image:v2"
	fp, err := projectFingerprint(proj, opts.DB)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeStateFile(diffResultsDir, opts.DB, diffCacheEntry{
		recordedState: recordedState{
			Host: opts.Host, Port: opts.Port, TargetID: "env-a/image:v1", Fingerprint: fp,
		},
		Result: DiffResult{DB: opts.DB, Created: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := FastDiff(context.Background(), opts, io.Discard); ok {
		t.Error("engine result from another env/image must never be replayed")
	}
}

// A record whose target matches but whose database can't be reached must
// fail open: better a slow engine diff (which will surface the real
// connection error) than a fabricated fast answer.
func TestFastDiff_DBUnreachableFallsThrough(t *testing.T) {
	proj := writeProj(t, "CREATE TABLE X (id INT)")
	opts := statePublishOpts(t, proj)
	fp, err := projectFingerprint(proj, opts.DB)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeStateFile(publishStateDir, opts.DB, publishState{
		recordedState: recordedState{Host: opts.Host, Port: opts.Port, Fingerprint: fp},
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, ok := FastDiff(ctx, opts, io.Discard); ok {
		t.Error("unreachable database must fall through, never answer fast")
	}
}

// writeStateFiles snapshots a project dir into the per-file inventory
// compareFiles diffs against.
func inventoryOf(t *testing.T, proj string) map[string]fileState {
	t.Helper()
	files, err := collectSourceFiles(proj)
	if err != nil {
		t.Fatal(err)
	}
	inv := make(map[string]fileState, len(files))
	for _, f := range files {
		sha, err := hashFile(f.Abs)
		if err != nil {
			t.Fatal(err)
		}
		inv[f.Abs] = fileState{Size: f.Size, MtimeNs: f.MtimeNs, SHA: sha}
	}
	return inv
}

func TestCompareFiles_DetectsAddModifyDelete(t *testing.T) {
	proj := writeProj(t, "CREATE TABLE X (id INT)")
	dir := filepath.Dir(proj)
	recorded := inventoryOf(t, proj)

	// Modify one, add one, delete via a recorded phantom entry.
	if err := os.WriteFile(filepath.Join(dir, "Table.sql"), []byte("CREATE TABLE X (id BIGINT)"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "Table.sql"), future, future)
	if err := os.WriteFile(filepath.Join(dir, "Proc.sql"), []byte("CREATE PROCEDURE P AS SELECT 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	recorded[filepath.ToSlash(filepath.Join(dir, "Gone.sql"))] = fileState{Size: 5, MtimeNs: 1, SHA: "x"}

	current, err := collectSourceFiles(proj)
	if err != nil {
		t.Fatal(err)
	}
	changes, churned, err := compareFiles(recorded, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(churned) != 0 {
		t.Errorf("no churn expected, got %d", len(churned))
	}
	// Order: Deleted → Modified → Added.
	if len(changes) != 3 ||
		changes[0].Action != "Deleted" || filepath.Base(changes[0].Path) != "Gone.sql" ||
		changes[1].Action != "Modified" || filepath.Base(changes[1].Path) != "Table.sql" ||
		changes[2].Action != "Added" || filepath.Base(changes[2].Path) != "Proc.sql" {
		t.Errorf("unexpected changes: %+v", changes)
	}
}

// A rewrite that restores identical content with a new mtime (pull,
// checkout) is churn, not change — it must not surface as Modified.
func TestCompareFiles_MtimeChurnIsNotAChange(t *testing.T) {
	proj := writeProj(t, "CREATE TABLE X (id INT)")
	dir := filepath.Dir(proj)
	recorded := inventoryOf(t, proj)

	if err := os.WriteFile(filepath.Join(dir, "Table.sql"), []byte("CREATE TABLE X (id INT)"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "Table.sql"), future, future); err != nil {
		t.Fatal(err)
	}

	current, err := collectSourceFiles(proj)
	if err != nil {
		t.Fatal(err)
	}
	changes, churned, err := compareFiles(recorded, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("identical content must not report changes, got %+v", changes)
	}
	if len(churned) != 1 || filepath.Base(churned[0].Abs) != "Table.sql" {
		t.Errorf("the rewritten file must be reported as churn, got %+v", churned)
	}
}

func TestDiffResultSummary_Wording(t *testing.T) {
	quick := DiffResult{DB: "AppDB", InSync: true, Quick: true}
	if got, want := quick.Summary(), "in sync — unchanged since last publish"; got != want {
		t.Errorf("quick summary = %q; want %q", got, want)
	}
	engine := DiffResult{DB: "AppDB", InSync: true}
	if got, want := engine.Summary(), "in sync — no schema changes"; got != want {
		t.Errorf("engine summary = %q; want %q", got, want)
	}
	files := DiffResult{DB: "AppDB", Quick: true, FileChanges: []FileChange{
		{Action: "Deleted", Path: "a.sql"},
		{Action: "Modified", Path: "b.sql"},
		{Action: "Added", Path: "c.sql"},
	}}
	if got, want := files.Summary(), "3 source file(s) changed since last publish (1 deleted, 1 modified, 1 added)"; got != want {
		t.Errorf("file-change summary = %q; want %q", got, want)
	}
}
