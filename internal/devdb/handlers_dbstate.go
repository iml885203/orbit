package devdb

import (
	"encoding/json"
	"net/http"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/dbstate"
)

// DBStateEventRequest is the body of POST /api/db-state/event.
// CLI calls this after a DB operation (apply, reset, publish,
// publish_clean, snapshot) succeeds or fails.
type DBStateEventRequest struct {
	Kind       string         `json:"kind"` // "apply" | "reset" | "publish" | "publish_clean" | "snapshot"
	DB         string         `json:"db"`
	Source     dbstate.Source `json:"source"` // "ui" | "cli"
	Status     string         `json:"status"` // "ok" | "error"
	DurationMs int64          `json:"durationMs,omitempty"`
	ErrorMsg   string         `json:"errorMsg,omitempty"`
}

func registerDBStateHandlers(mux *http.ServeMux, f *dbFeature) {
	// Reads f.dbState (assigned by daemonSetup before registration).
	// GET stays ungated: it reads an (empty) snapshot and the dashboard
	// streams it unconditionally. Only the write path is part of the DB
	// workflow surface.
	mux.HandleFunc("/api/db-state", func(w http.ResponseWriter, r *http.Request) {
		if daemon.RequireMethod(w, r, http.MethodGet) {
			return
		}
		daemon.WriteJSON(w, http.StatusOK, f.dbState.Snapshot())
	})

	mux.HandleFunc("/api/db-state/event", func(w http.ResponseWriter, r *http.Request) {
		if daemon.RequireMethod(w, r, http.MethodPost) {
			return
		}
		if f.rejectIfDBNotConfigured(w) {
			return
		}
		var req DBStateEventRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "invalid json"})
			return
		}
		if req.DB == "" || req.Kind == "" || req.Source == "" {
			daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "db, kind, source required"})
			return
		}
		// status=error events do not mutate state — they live in history audit only.
		if req.Status != "ok" {
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true})
			return
		}
		var err error
		switch req.Kind {
		case "apply":
			err = f.dbState.Apply(req.DB, req.Source, req.DurationMs)
		case "reset":
			err = f.dbState.Reset(req.DB, req.Source, req.DurationMs)
		case "publish":
			err = f.dbState.Publish(req.DB, req.Source, req.DurationMs)
		case "publish_clean":
			err = f.dbState.PublishClean(req.DB, req.Source, req.DurationMs)
		case "snapshot":
			err = f.dbState.SnapshotRefreshed(req.DB, req.Source)
		default:
			daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "kind must be apply|reset|publish|publish_clean|snapshot"})
			return
		}
		// Any schema-mutating op invalidates the DB's cached drift — the
		// last diff no longer describes the live schema. Snapshot refresh
		// mutates nothing.
		if req.Kind != "snapshot" {
			f.drift.markStale(req.DB)
		}
		if err != nil {
			daemon.WriteJSON(w, http.StatusInternalServerError, daemon.APIResponse{Error: err.Error()})
			return
		}
		daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true})
	})

}
