package devdb

// The daemon side of `db publish`: the HTTP endpoint and the operation
// runner riding the shared dbops lock/SSE machinery. The CLI twin lives
// in db_publish.go; project/target resolution in db_projects.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/dbstate"
	"github.com/iml885203/orbit/internal/sqlpublish"
)

func registerDBPublishHandler(mux *http.ServeMux, f *dbFeature) {
	mux.HandleFunc("/api/db/publish", func(w http.ResponseWriter, r *http.Request) {
		if daemon.RequireMethod(w, r, http.MethodPost) {
			return
		}
		f.handleDBOpPublish(w, r)
	})
}

// DBPublishRequest is the body for POST /api/db/publish.
type DBPublishRequest struct {
	// DB names one database; empty and ignored when All is set.
	DB    string `json:"db,omitempty"`
	Force bool   `json:"force,omitempty"`
	// All publishes every database from the project merge, sequentially
	// under the one op lock, stopping at the first failure.
	All bool `json:"all,omitempty"`
}

func (f *dbFeature) handleDBOpPublish(w http.ResponseWriter, r *http.Request) {
	if f.rejectIfDBNotConfigured(w) {
		return
	}
	var req DBPublishRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "invalid json"})
		return
	}
	if !req.All && !safeDBName.MatchString(req.DB) {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "invalid db name"})
		return
	}
	if req.All && req.DB != "" {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "all and db are mutually exclusive"})
		return
	}
	var targets []publishTargetRef
	if req.All {
		var err error
		if targets, err = f.allPublishTargets(); err != nil {
			daemon.WriteJSON(w, http.StatusInternalServerError, daemon.APIResponse{Error: err.Error()})
			return
		}
		if len(targets) == 0 {
			daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "no databases found — check sqlserver.projects in the active environment"})
			return
		}
	} else {
		sqlProj, err := f.resolveSQLProj(req.DB)
		if err != nil {
			daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: err.Error()})
			return
		}
		targets = []publishTargetRef{{DB: req.DB, SQLProj: sqlProj}}
	}
	host, port, username, password, ok := f.resolveSQLServerConn(w)
	if !ok {
		return
	}

	if !f.lockDBOpOrConflict(w, dbOpPublish, req.DB, req.All) {
		return
	}

	go f.runPublishOp(sqlpublish.Opts{
		Host:     host,
		Port:     port,
		TargetID: f.publishTargetID(),
		User:     username,
		Password: password,
		Force:    req.Force,
	}, targets)
	daemon.WriteJSON(w, http.StatusAccepted, daemon.APIResponse{OK: true})
}

// resolveSQLServerConn resolves the configured target's host, port, and
// credentials for a host-side publish or reset. It writes the matching HTTP
// error and returns ok=false when the target is missing from the env,
// not running, or has no usable SA password. Shared by the publish and
// reset handlers so both gate connectivity identically.
func (f *dbFeature) resolveSQLServerConn(w http.ResponseWriter) (host string, port int, username, password string, ok bool) {
	target, targetName, found := f.publishTarget()
	if !found {
		daemon.WriteJSON(w, http.StatusServiceUnavailable, daemon.APIResponse{Error: "publish target container not found in the active env"})
		return
	}
	if !containerRunning(dbTargetDockerName(targetName)) {
		daemon.WriteJSON(w, http.StatusServiceUnavailable, daemon.APIResponse{Error: targetName + " is not running"})
		return
	}
	port, err := publishTargetHostPort(target)
	if err != nil {
		daemon.WriteJSON(w, http.StatusServiceUnavailable, daemon.APIResponse{Error: err.Error()})
		return
	}
	section := SQLServerFrom(f.host.Config())
	if section == nil {
		daemon.WriteJSON(w, http.StatusServiceUnavailable, daemon.APIResponse{Error: ErrMsgDBNotConfigured})
		return
	}
	password = target.Environment[section.PasswordEnv]
	if password == "" || strings.Contains(password, "${") {
		daemon.WriteJSON(w, http.StatusServiceUnavailable, daemon.APIResponse{Error: section.PasswordEnv + " is unresolved for " + targetName})
		return
	}
	return "localhost", port, section.Username, password, true
}

// runPublishOp publishes targets sequentially under the already-held op
// lock, stopping at the first failure (a broken schema early in the
// list is a reason to look, not to plough on). base carries the shared
// connection; per-target DB/SQLProj are filled in per iteration.
func (f *dbFeature) runPublishOp(base sqlpublish.Opts, targets []publishTargetRef) {
	totalMs := int64(0)
	for i, t := range targets {
		if len(targets) > 1 {
			_, _ = fmt.Fprintf(f.dbOps, "[%d/%d] publishing %s\n", i+1, len(targets), t.DB)
		}
		opts := base
		opts.DB = t.DB
		opts.SQLProj = t.SQLProj
		res := runSQLPublish(opts, false, f.dbOps)
		totalMs += res.DurationMs
		if !res.OK {
			errMsg := t.DB + ": "
			if res.Err != nil {
				errMsg += res.Err.Error()
			}
			if len(targets) > 1 {
				errMsg = fmt.Sprintf("%s (%d of %d published, the rest skipped)", errMsg, i, len(targets))
			}
			f.dbOps.Finish(false, totalMs, errMsg, string(res.Code))
			return
		}
		_ = f.dbState.Publish(t.DB, dbstate.SourceUI, res.DurationMs)
		if res.Created {
			f.autoBaseline(opts)
		}
		f.drift.markStale(t.DB)
	}
	f.dbOps.Finish(true, totalMs, "", "")
}

// autoBaseline declares a just-created database's clean contents as its
// baseline — the point reset reverts to. It runs only after a publish
// that brought the DB into existence (Result.Created), the one moment
// the DB is guaranteed clean (schema + reference data, no test data), so
// the user never has to think about snapshots. opts carries the DB name
// and connection. A failure is logged, not fatal: the publish already
// succeeded, and reset will re-declare the baseline on its first run.
func (f *dbFeature) autoBaseline(opts sqlpublish.Opts) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := sqlpublish.RefreshBaseline(ctx, opts, opts.DB, f.dbOps); err != nil {
		_, _ = fmt.Fprintf(f.dbOps, "[baseline] auto-baseline of %s failed: %v\n", opts.DB, err)
		return
	}
	_ = f.dbState.SnapshotRefreshed(opts.DB, dbstate.SourceUI)
}

// lockDBOpOrConflict claims the single db-op slot, answering 409 with
// the in-flight op when another operation holds it.
func (f *dbFeature) lockDBOpOrConflict(w http.ResponseWriter, kind dbOpKind, db string, all bool) bool {
	if f.dbOps.LockOrReject(kind, db, all) {
		return true
	}
	k, inFlight := f.dbOps.InFlight()
	daemon.WriteJSON(w, http.StatusConflict, daemon.APIResponse{Error: fmt.Sprintf("another db operation in progress: %s %s", k, inFlight)})
	return false
}

// resolveSQLProj maps a database name to its .sqlproj through the
// daemon's own project merge.
func (f *dbFeature) resolveSQLProj(db string) (string, error) {
	projects, err := f.allProjects()
	if err != nil {
		return "", err
	}
	if proj, ok := sqlProjForDatabase(projects, db); ok {
		return proj, nil
	}
	return "", fmt.Errorf("database %q not found in sqlserver.projects", db)
}
