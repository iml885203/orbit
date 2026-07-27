package devdb

// The daemon side of `db diff`: POST /api/db/diff, a synchronous,
// read-only schema-diff query. It builds the project's dacpac and asks
// sqlpackage what a publish would change, then returns the structured
// DiffResult. Unlike publish/reset it does NOT take the dbops lock —
// a diff mutates nothing, so it must not block (or be blocked by) a
// real publish. The CLI twin lives in db_diff.go.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/sqlpublish"
)

func registerDBDiffHandler(mux *http.ServeMux, f *dbFeature) {
	mux.HandleFunc("/api/db/diff", func(w http.ResponseWriter, r *http.Request) {
		if daemon.RequireMethod(w, r, http.MethodPost) {
			return
		}
		f.handleDBDiff(w, r)
	})
	// The drift cache read: every DB's last diff outcome, so a fresh page
	// load restores its badges instead of starting blank.
	mux.HandleFunc("/api/db/drift", func(w http.ResponseWriter, r *http.Request) {
		if daemon.RequireMethod(w, r, http.MethodGet) {
			return
		}
		daemon.WriteJSON(w, http.StatusOK, DriftSnapshotResponse{Entries: f.drift.snapshot()})
	})
}

// DBDiffRequest is the body for POST /api/db/diff.
type DBDiffRequest struct {
	DB string `json:"db"`
	// Script requests the full T-SQL deployment script instead of the
	// structured operation summary.
	Script bool `json:"script,omitempty"`
	// Analyze compares the dacpac with SQL Server to return exact object
	// operations and data-loss warnings.
	Analyze bool `json:"analyze,omitempty"`
	// FastOnly returns needs_engine instead of falling through to
	// sqlpackage. Page-entry refreshes use it so navigation stays cheap.
	FastOnly bool `json:"fast_only,omitempty"`
}

// DBDiffResponse carries a structured result, a requested SQL script, or
// NeedsEngine when a fast-only request cannot prove an answer.
type DBDiffResponse struct {
	Result      *sqlpublish.DiffResult `json:"result,omitempty"`
	Script      string                 `json:"script,omitempty"`
	NeedsEngine bool                   `json:"needs_engine,omitempty"`
}

func (f *dbFeature) handleDBDiff(w http.ResponseWriter, r *http.Request) {
	if f.rejectIfDBNotConfigured(w) {
		return
	}
	var req DBDiffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "invalid request body: " + err.Error()})
		return
	}
	if !safeDBName.MatchString(req.DB) {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "invalid database name"})
		return
	}

	host, port, username, password, ok := f.resolveSQLServerConn(w)
	if !ok {
		return
	}
	sqlProj, err := f.resolveSQLProj(req.DB)
	if err != nil {
		daemon.WriteJSON(w, http.StatusNotFound, daemon.APIResponse{Error: err.Error()})
		return
	}

	base := sqlpublish.Opts{
		DB:       req.DB,
		SQLProj:  sqlProj,
		Host:     host,
		Port:     port,
		TargetID: f.publishTargetID(),
		User:     username,
		Password: password,
		Analyze:  req.Analyze,
	}
	if req.FastOnly {
		generation := f.drift.currentGeneration(req.DB)
		if result, found := sqlpublish.FastDiff(r.Context(), base, io.Discard); found {
			f.drift.recordIfCurrent(req.DB, generation, &result, sqlpublish.CodeNone, nil)
			daemon.WriteJSON(w, http.StatusOK, DBDiffResponse{Result: &result})
			return
		}
		daemon.WriteJSON(w, http.StatusOK, DBDiffResponse{NeedsEngine: true})
		return
	}

	// Build output is discarded on the wire path (the client shows a
	// spinner, not build logs); failures surface via the returned code.
	if req.Script {
		var script string
		var code sqlpublish.ErrorCode
		var runErr error
		withPublishScratch(base, func(ctx context.Context, o sqlpublish.Opts) sqlpublish.Result {
			script, code, runErr = sqlpublish.DiffScript(ctx, o, io.Discard)
			return sqlpublish.Result{OK: runErr == nil, Code: code, Err: runErr}
		})
		if runErr != nil {
			daemon.WriteJSON(w, diffHTTPStatus(code), daemon.APIResponse{Error: runErr.Error(), Code: string(code)})
			return
		}
		daemon.WriteJSON(w, http.StatusOK, DBDiffResponse{Script: script})
		return
	}

	result, code, runErr := f.runAndRecordDiff(base)
	if runErr != nil {
		daemon.WriteJSON(w, diffHTTPStatus(code), daemon.APIResponse{Error: runErr.Error(), Code: string(code)})
		return
	}
	daemon.WriteJSON(w, http.StatusOK, DBDiffResponse{Result: &result})
}

func (f *dbFeature) runAndRecordDiff(opts sqlpublish.Opts) (sqlpublish.DiffResult, sqlpublish.ErrorCode, error) {
	generation := f.drift.currentGeneration(opts.DB)
	runner := f.diffRunner
	if runner == nil {
		runner = runDiff
	}
	result, code, err := runner(opts)
	if err != nil {
		f.drift.recordIfCurrent(opts.DB, generation, nil, code, err)
		return result, code, err
	}
	f.drift.recordIfCurrent(opts.DB, generation, &result, sqlpublish.CodeNone, nil)
	return result, code, nil
}

// runDiff runs one read-only schema diff inside a publish scratch dir.
// Build output is discarded (the wire path shows a spinner, not logs);
// failures surface via the returned code. The dashboard's Check-all fans
// out over this same endpoint with a client-side pool, so per-request
// isolation is all the concurrency control the daemon needs.
func runDiff(opts sqlpublish.Opts) (sqlpublish.DiffResult, sqlpublish.ErrorCode, error) {
	var result sqlpublish.DiffResult
	var code sqlpublish.ErrorCode
	var runErr error
	withPublishScratch(opts, func(ctx context.Context, o sqlpublish.Opts) sqlpublish.Result {
		result, code, runErr = sqlpublish.Diff(ctx, o, io.Discard)
		return sqlpublish.Result{OK: runErr == nil, Code: code, Err: runErr}
	})
	return result, code, runErr
}

// diffHTTPStatus maps a diff failure code to an HTTP status: a missing
// toolchain / project / unreachable server is a 503-ish precondition,
// everything else a 500.
func diffHTTPStatus(code sqlpublish.ErrorCode) int {
	switch code {
	case sqlpublish.CodeToolchainMissing,
		sqlpublish.CodeSQLProjectNotFound,
		sqlpublish.CodeSQLServerUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
