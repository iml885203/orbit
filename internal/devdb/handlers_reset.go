package devdb

// The daemon side of `db reset`: POST /api/db/reset and the reset op
// runner riding the shared dbops lock/SSE machinery. The CLI twin lives
// in db_reset.go. The server owns the standard-vs-recreate decision from
// the live database — the client never selects the destructive path.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/dbstate"
	"github.com/iml885203/orbit/internal/sqlpublish"
)

// CodeResetRequiresConfirmation is returned when reset needs explicit
// acknowledgement that local data will be discarded.
const CodeResetRequiresConfirmation = "reset_requires_confirmation"

func registerDBResetHandler(mux *http.ServeMux, f *dbFeature) {
	mux.HandleFunc("/api/db/reset", func(w http.ResponseWriter, r *http.Request) {
		if daemon.RequireMethod(w, r, http.MethodPost) {
			return
		}
		f.handleDBOpReset(w, r)
	})
	mux.HandleFunc("/api/db/reset-state", func(w http.ResponseWriter, r *http.Request) {
		if daemon.RequireMethod(w, r, http.MethodGet) {
			return
		}
		f.handleDBResetState(w, r)
	})
}

// DBResetState lets clients explain the reset path before asking for
// destructive confirmation. A database without a baseline must be recreated.
type DBResetState struct {
	Exists      bool `json:"exists"`
	HasBaseline bool `json:"hasBaseline"`
}

// DBResetStateResponse maps each known database to its reset readiness.
type DBResetStateResponse struct {
	States map[string]DBResetState `json:"states"`
}

func (f *dbFeature) handleDBResetState(w http.ResponseWriter, r *http.Request) {
	if f.rejectIfDBNotConfigured(w) {
		return
	}
	targets, err := f.allPublishTargets()
	if err != nil {
		daemon.WriteJSON(w, http.StatusInternalServerError, daemon.APIResponse{Error: err.Error()})
		return
	}
	host, port, username, password, ok := f.resolveSQLServerConn(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	states := make(map[string]DBResetState, len(targets))
	for _, t := range targets {
		opts := sqlpublish.Opts{DB: t.DB, Host: host, Port: port, User: username, Password: password}
		exists, err := sqlpublish.DatabaseExists(ctx, opts)
		if err != nil {
			// A transient per-DB probe error: omit it rather than fail the
			// whole page — the UI treats a missing entry as unknown and
			// falls back to the server-authoritative 409 gate on click.
			continue
		}
		if !exists {
			states[t.DB] = DBResetState{Exists: false}
			continue
		}
		hasBaseline, err := sqlpublish.BaselineExists(ctx, opts, t.DB)
		if err != nil {
			continue
		}
		states[t.DB] = DBResetState{Exists: true, HasBaseline: hasBaseline}
	}
	daemon.WriteJSON(w, http.StatusOK, DBResetStateResponse{States: states})
}

// DBResetRequest is the body for POST /api/db/reset. The client never
// selects the reset path. AcknowledgeDataLoss confirms the caller showed
// that reset discards local data; the server still chooses the fastest
// safe implementation.
type DBResetRequest struct {
	DB                  string `json:"db"`
	AcknowledgeDataLoss bool   `json:"acknowledgeDataLoss,omitempty"`
}

func (f *dbFeature) handleDBOpReset(w http.ResponseWriter, r *http.Request) {
	if f.rejectIfDBNotConfigured(w) {
		return
	}
	var req DBResetRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "invalid json"})
		return
	}
	if !safeDBName.MatchString(req.DB) {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "invalid db name"})
		return
	}
	sqlProj, err := f.resolveSQLProj(req.DB)
	if err != nil {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: err.Error()})
		return
	}
	host, port, username, password, ok := f.resolveSQLServerConn(w)
	if !ok {
		return
	}
	opts := sqlpublish.Opts{DB: req.DB, SQLProj: sqlProj, Host: host, Port: port, TargetID: f.publishTargetID(), User: username, Password: password}

	// The server owns the standard-vs-recreate decision from the live
	// database. A DB that doesn't exist can't be reset (publish creates
	// it). A DB with no baseline can only be reset by a destructive
	// from-scratch rebuild, which the caller must acknowledge — never
	// escalate destructiveness on a request that didn't opt in.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	exists, err := sqlpublish.DatabaseExists(ctx, opts)
	if err != nil {
		daemon.WriteJSON(w, http.StatusServiceUnavailable, daemon.APIResponse{Error: "checking database: " + err.Error()})
		return
	}
	if !exists {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "publish this database before resetting it"})
		return
	}
	hasBaseline, err := sqlpublish.BaselineExists(ctx, opts, req.DB)
	if err != nil {
		daemon.WriteJSON(w, http.StatusServiceUnavailable, daemon.APIResponse{Error: "checking baseline: " + err.Error()})
		return
	}
	if !hasBaseline && !req.AcknowledgeDataLoss {
		daemon.WriteJSON(w, http.StatusConflict, daemon.APIResponse{
			Error: "reset discards all local data — confirm the reset and retry",
			Code:  CodeResetRequiresConfirmation,
		})
		return
	}

	if !f.lockDBOpOrConflict(w, dbOpReset, req.DB, false) {
		return
	}
	// The acknowledgement authorizes a from-scratch rebuild if the saved
	// clean state is unavailable now or disappears before the worker runs.
	go f.runResetOp(opts, req.AcknowledgeDataLoss)
	daemon.WriteJSON(w, http.StatusAccepted, daemon.APIResponse{OK: true})
}

// runResetOp runs one reset under the held op lock and records the
// outcome: the reset event plus, on success, the clean-publish state
// (latest schema published, baseline refreshed) so the dashboard shows
// the DB published with a baseline and drops any legacy notice.
func (f *dbFeature) runResetOp(opts sqlpublish.Opts, allowRecreate bool) {
	res := runSQLReset(opts, allowRecreate, f.dbOps)
	if !res.OK {
		errMsg := opts.DB + ": "
		if res.Err != nil {
			errMsg += res.Err.Error()
		}
		f.dbOps.Finish(false, res.DurationMs, errMsg, string(res.Code))
		return
	}
	_ = f.dbState.Reset(opts.DB, dbstate.SourceUI, res.DurationMs)
	_ = f.dbState.PublishClean(opts.DB, dbstate.SourceUI, res.DurationMs)
	f.drift.markStale(opts.DB)
	f.dbOps.Finish(true, res.DurationMs, "", "")
}
