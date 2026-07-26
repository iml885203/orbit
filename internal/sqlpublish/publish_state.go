package sqlpublish

// Publish-state record + diff-result cache — the fast half of `db diff`.
//
// Every successful schema-advancing publish stores (a) the project
// fingerprint it built from, (b) a per-file inventory (size/mtime/sha256)
// of the source set, and (c) cheap DB-side markers of the just-published
// database. Every successful ENGINE diff additionally stores its full
// DiffResult keyed by the same (fingerprint, markers) pair.
//
// Diff consults these before the engine, cheapest sufficient answer wins:
//
//	Tier 1  fingerprint + markers match the publish record
//	        → "in sync — unchanged since last publish", milliseconds.
//	Tier 2  fingerprint + markers match the last engine run
//	        → replay its exact operations from the cache, milliseconds.
//	Tier 3  markers match the publish record but source files moved
//	        → per-file added/modified/deleted list (content-hash based,
//	          so pull/checkout mtime churn self-heals back to Tier 1),
//	          milliseconds. Approximate: names WHAT changed, not the
//	          exact T-SQL operations — the engine (or Tier 2) does that.
//
// Any mismatch, missing record, or error falls through to the full
// engine diff (fail-open: the fast paths can only ever skip work, never
// invent a wrong answer).
//
// What the DB markers can and can't see: create_date catches a
// recreated database (fresh volume/container); object count + max
// modify_date catch DDL applied behind orbit's back (manual ALTER /
// CREATE / DROP — index changes bump the parent object's modify_date
// too). Permission-only drift (GRANT/REVOKE) does not move sys.objects
// and is invisible to the indexed paths — Opts.Analyze forces the engine
// diff for that.
//
// Note the deliberate semantic difference from the engine diff: Tier 1
// "in sync" means "nothing changed since the last publish", not "a
// publish would be a no-op". For projects where sqlpackage perpetually
// reports normalization-only operations, Tier 1 reports clean once
// published — DiffResult.Quick lets callers phrase that honestly. Tier
// 1 deliberately outranks a Tier 2 entry recording those operations.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/iml885203/orbit/atomicio"
)

// dbMarkerSet is the cheap DB-side drift signal, shared by the publish
// record and the diff-result cache entry.
type dbMarkerSet struct {
	DBCreateDate  string `json:"db_create_date"`  // sys.databases.create_date, ISO 8601
	ObjectCount   int64  `json:"object_count"`    // non-ms-shipped objects in the DB
	MaxModifyDate string `json:"max_modify_date"` // max sys.objects.modify_date, ISO 8601
}

// fileState is one source file's recorded identity: cheap signals to
// skip hashing (size/mtime) plus the content hash that decides when the
// cheap signals lie (pull/checkout rewrites mtime without the content).
type fileState struct {
	Size    int64  `json:"size"`
	MtimeNs int64  `json:"mtime_ns"`
	SHA     string `json:"sha"`
}

type recordedState struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	TargetID    string `json:"target_id"`
	Fingerprint string `json:"fingerprint"`
	dbMarkerSet
	At string `json:"at"`
}

func (s recordedState) matchesTarget(opts Opts) bool {
	return s.Host == opts.Host && s.Port == opts.Port && s.TargetID == opts.TargetID
}

// publishState is one database's recorded publish outcome.
type publishState struct {
	recordedState
	Files map[string]fileState `json:"files,omitempty"` // abs slash path → state
}

// diffCacheEntry is the last engine diff's result plus the exact state
// it was computed against; a later diff in the same state replays it.
type diffCacheEntry struct {
	recordedState
	Result DiffResult `json:"result"`
}

// dbMarkers reads the current DB-side drift markers for opts.DB. Two
// catalog reads, milliseconds against a healthy local server.
func dbMarkers(ctx context.Context, opts Opts) (dbMarkerSet, error) {
	if !safeIdent.MatchString(opts.DB) {
		return dbMarkerSet{}, fmt.Errorf("invalid database name %q", opts.DB)
	}
	conn, err := openMasterDB(opts)
	if err != nil {
		return dbMarkerSet{}, err
	}
	defer func() { _ = conn.Close() }()

	var m dbMarkerSet
	if err := conn.QueryRowContext(ctx,
		`SELECT CONVERT(varchar(33), create_date, 126) FROM sys.databases WHERE name = @p1`,
		opts.DB).Scan(&m.DBCreateDate); err != nil {
		return dbMarkerSet{}, fmt.Errorf("reading create_date of %s: %w", opts.DB, err)
	}
	// Cross-database catalog read; the DB name passed safeIdent above.
	// ISNULL floor keeps an empty database (no user objects) comparable.
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*), CONVERT(varchar(33), ISNULL(MAX(modify_date), '19000101'), 126)
		 FROM [`+opts.DB+`].sys.objects WHERE is_ms_shipped = 0`).
		Scan(&m.ObjectCount, &m.MaxModifyDate); err != nil {
		return dbMarkerSet{}, fmt.Errorf("reading object markers of %s: %w", opts.DB, err)
	}
	return m, nil
}

// The two record kinds, as orbit cache subdirectories.
const (
	publishStateDir = "publish-state"
	diffResultsDir  = "diff-results"
)

// statePath returns db's record file under the named orbit cache dir.
func statePath(sub, db string) (string, error) {
	dir, err := orbitCacheDir(sub)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, db+".json"), nil
}

// writeStateFile marshals v to the record file, replacing any previous.
// Atomic write: a concurrent diff must never json.Unmarshal a torn file
// (publish and diff run as separate overlapping processes).
func writeStateFile(sub, db string, v any) error {
	path, err := statePath(sub, db)
	if err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return atomicio.WriteFile(path, data, 0o644)
}

// loadStateFile unmarshals db's record into v; false means "no usable
// record".
func loadStateFile(sub, db string, v any) bool {
	path, err := statePath(sub, db)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

func loadPublishState(db string) (publishState, bool) {
	var st publishState
	if !loadStateFile(publishStateDir, db, &st) || st.Fingerprint == "" {
		return publishState{}, false
	}
	return st, true
}

func loadDiffCache(db string) (diffCacheEntry, bool) {
	var ce diffCacheEntry
	if !loadStateFile(diffResultsDir, db, &ce) || ce.Fingerprint == "" {
		return diffCacheEntry{}, false
	}
	return ce, true
}

// hashFile returns the sha256 hex of a file's content.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// recordPublishState captures the just-published project + database
// state for opts.DB, including the per-file inventory Tier 2 diffs
// against. Called at the tail of every successful publish; best-effort —
// the caller logs a failure and moves on (the only cost is that the
// next diff runs the full engine).
func recordPublishState(ctx context.Context, opts Opts, expectedFingerprint string) error {
	files, err := collectSourceFiles(opts.SQLProj)
	if err != nil {
		return err
	}
	fingerprint := fingerprintFiles(files, opts.DB)
	if fingerprint != expectedFingerprint {
		return fmt.Errorf("source changed after build")
	}
	inventory := make(map[string]fileState, len(files))
	for _, f := range files {
		sha, err := hashFile(f.Abs)
		if err != nil {
			return err
		}
		inventory[f.Abs] = fileState{Size: f.Size, MtimeNs: f.MtimeNs, SHA: sha}
	}
	markers, err := dbMarkers(ctx, opts)
	if err != nil {
		return err
	}
	return writeStateFile(publishStateDir, opts.DB, publishState{
		recordedState: recordedState{
			Host: opts.Host, Port: opts.Port, TargetID: opts.TargetID,
			Fingerprint: fingerprint, dbMarkerSet: markers, At: time.Now().Format(time.RFC3339),
		},
		Files: inventory,
	})
}

// recordPublishStateBestEffort is recordPublishState for the tail of a
// successful publish: failures are logged, never fatal — losing the
// record only costs the next diff an engine run. Shared by Publish and
// PublishClean so the two tails can't drift.
func recordPublishStateBestEffort(ctx context.Context, opts Opts, expectedFingerprint string, out io.Writer) {
	if err := recordPublishState(ctx, opts, expectedFingerprint); err != nil {
		fmt.Fprintf(out, "[state] publish state not recorded (next diff runs the full engine): %v\n", err)
	}
}

// recordDiffCache stores an engine diff's result keyed by the state it
// ran against, so an identical later state replays it instead of paying
// the engine again. Best-effort, same as recordPublishState. The state
// is captured AFTER the engine run — a concurrent edit or publish just
// produces a key that never matches, i.e. a cache miss, never a wrong
// replay.
func recordDiffCache(ctx context.Context, opts Opts, expectedFingerprint string, expectedMarkers dbMarkerSet, result DiffResult) error {
	fingerprint, err := projectFingerprint(opts.SQLProj, opts.DB)
	if err != nil {
		return err
	}
	if fingerprint != expectedFingerprint {
		return fmt.Errorf("source changed during diff")
	}
	markers, err := dbMarkers(ctx, opts)
	if err != nil {
		return err
	}
	if markers != expectedMarkers {
		return fmt.Errorf("database changed during diff")
	}
	return writeStateFile(diffResultsDir, opts.DB, diffCacheEntry{
		recordedState: recordedState{
			Host: opts.Host, Port: opts.Port, TargetID: opts.TargetID,
			Fingerprint: fingerprint, dbMarkerSet: markers, At: time.Now().Format(time.RFC3339),
		},
		Result: result,
	})
}

// compareFiles diffs the current source set against the recorded
// inventory. Files whose size+mtime match are trusted unchanged; the
// rest are content-hashed, so a pull/checkout that rewrote mtimes but
// not content lands in churned (no real change) instead of changes.
// A file that can't be hashed poisons the fast answer — the error makes
// the caller fall through to the engine.
func compareFiles(recorded map[string]fileState, current []sourceFile) (changes []FileChange, churned []sourceFile, err error) {
	seen := make(map[string]bool, len(current))
	for _, f := range current {
		seen[f.Abs] = true
		rec, ok := recorded[f.Abs]
		if !ok {
			changes = append(changes, FileChange{Action: "Added", Path: displayPath(f)})
			continue
		}
		if f.Size == rec.Size && f.MtimeNs == rec.MtimeNs {
			continue
		}
		sha, hashErr := hashFile(f.Abs)
		if hashErr != nil {
			return nil, nil, hashErr
		}
		if sha == rec.SHA {
			churned = append(churned, f)
			continue
		}
		changes = append(changes, FileChange{Action: "Modified", Path: displayPath(f)})
	}
	// Deleted files aren't in the current walk, so recover their source
	// root from the surviving files' roots (a vanished file usually
	// leaves its project directory behind).
	roots := map[string]bool{}
	for _, f := range current {
		roots[f.Root] = true
	}
	for abs := range recorded {
		if !seen[abs] {
			changes = append(changes, FileChange{Action: "Deleted", Path: deletedDisplayPath(abs, roots)})
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		ri, rj := ActionRank(changes[i].Action), ActionRank(changes[j].Action)
		if ri != rj {
			return ri < rj
		}
		return changes[i].Path < changes[j].Path
	})
	return changes, churned, nil
}

// displayPath renders a source file relative to its source root, prefixed
// with the root's name so files from referenced projects stay
// distinguishable (e.g. "PaymentDB/dbo/Tables/X.sql",
// "CommonFiles/Security/Role.sql").
func displayPath(f sourceFile) string {
	rel, err := filepath.Rel(f.Root, filepath.FromSlash(f.Abs))
	if err != nil {
		return f.Abs
	}
	return filepath.ToSlash(filepath.Join(filepath.Base(f.Root), rel))
}

// deletedDisplayPath renders a recorded-but-vanished file against the
// current source roots; a file whose whole project directory vanished
// falls back to its absolute path.
func deletedDisplayPath(abs string, roots map[string]bool) string {
	native := filepath.FromSlash(abs)
	for root := range roots {
		if rel, err := filepath.Rel(root, native); err == nil && filepath.IsLocal(rel) {
			return filepath.ToSlash(filepath.Join(filepath.Base(root), rel))
		}
	}
	return abs
}

// fastDiff answers Diff without the engine when a recorded state proves
// what the answer is. Tier order: exact beats approximate, so a cached
// engine result outranks the file-level list. Any doubt returns false
// and Diff falls through to the engine.
// FastDiff returns a cached or file-level answer without invoking dotnet or
// sqlpackage. ok=false means the recorded state cannot prove an answer; UI
// auto-refresh uses that signal to offer an explicit engine check instead of
// starting expensive work merely because the page became visible.
func FastDiff(ctx context.Context, opts Opts, out io.Writer) (DiffResult, bool) {
	st, stOK, ce, ceOK := matchingRecordedStates(opts)
	if !stOK && !ceOK {
		return DiffResult{}, false
	}

	files, err := collectSourceFiles(opts.SQLProj)
	if err != nil {
		return DiffResult{}, false
	}
	fingerprint := fingerprintFiles(files, opts.DB)
	markers, err := dbMarkers(ctx, opts)
	if err != nil {
		return DiffResult{}, false
	}

	// Tier 1: nothing changed since the last publish.
	if stOK && fingerprint == st.Fingerprint && markers == st.dbMarkerSet {
		return DiffResult{DB: opts.DB, InSync: true, Quick: true}, true
	}

	// When the database is still at the published state, preserve source
	// changes even if sqlpackage considers them a deployment no-op. File moves
	// and equivalent SQL rewrites must not disappear merely because they build
	// the same schema.
	if stOK && markers == st.dbMarkerSet && len(st.Files) > 0 {
		changes, churned, err := compareFiles(st.Files, files)
		if err != nil {
			return DiffResult{}, false
		}
		if len(changes) == 0 {
			selfHealChurn(opts.DB, st, churned, fingerprint, out)
			return DiffResult{DB: opts.DB, InSync: true, Quick: true}, true
		}
		if ceOK && fingerprint == ce.Fingerprint && markers == ce.dbMarkerSet {
			return replayCachedDiff(ce.Result, changes), true
		}
		return DiffResult{DB: opts.DB, Quick: true, FileChanges: changes}, true
	}

	// Tier 2 without a matching publish record: the exact same state as the
	// last engine run can still replay its database operations.
	if ceOK && fingerprint == ce.Fingerprint && markers == ce.dbMarkerSet {
		return replayCachedDiff(ce.Result, nil), true
	}

	return DiffResult{}, false
}

func matchingRecordedStates(opts Opts) (publishState, bool, diffCacheEntry, bool) {
	st, stOK := loadPublishState(opts.DB)
	if stOK && !st.matchesTarget(opts) {
		stOK = false // record describes a different server's database
	}
	ce, ceOK := loadDiffCache(opts.DB)
	if ceOK && !ce.matchesTarget(opts) {
		ceOK = false
	}
	return st, stOK, ce, ceOK
}

func replayCachedDiff(result DiffResult, changes []FileChange) DiffResult {
	result.Cached = true
	result.FileChanges = changes
	if len(changes) > 0 {
		result.InSync = false
	}
	return result
}

// selfHealChurn refreshes the publish record after a churn-only compare
// (content identical, mtimes rewritten — e.g. a pull that restored the
// same files), so the next diff takes Tier 1 without re-hashing.
// Best-effort: a failed write only costs the next diff the same re-hash.
func selfHealChurn(db string, st publishState, churned []sourceFile, fingerprint string, out io.Writer) {
	for _, f := range churned {
		st.Files[f.Abs] = fileState{Size: f.Size, MtimeNs: f.MtimeNs, SHA: st.Files[f.Abs].SHA}
	}
	st.Fingerprint = fingerprint
	if err := writeStateFile(publishStateDir, db, st); err != nil {
		fmt.Fprintf(out, "[diff] mtime-churn record refresh failed (harmless, next diff re-hashes): %v\n", err)
	}
}
