// The DB-workflow gate: envs opt into the DB workflow by defining a
// sql-server container (legacy convention) or by declaring a
// sql_projects publish target (the generic path). The predicate lives
// here — DBWorkflowConfigured below. Adopting teams without MSSQL never
// meet the SQL image's UI or errors — endpoints reject with
// ErrMsgDBNotConfigured, doctor collapses its DB checks into one skip line,
// and the dashboard hides the DB surfaces via /api/devdb/meta.

package devdb

import (
	"net/http"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

// ErrMsgDBNotConfigured is returned by every DB-workflow endpoint (and
// reused verbatim by the CLI) when the active env doesn't opt in.
const ErrMsgDBNotConfigured = "db workflow not configured: the active env declares neither a sql-server container nor sql_projects — see docs/team-adoption.md"

// sqlServerContainerConfig returns the active env's sql-server container
// from the current immutable snapshot. Named ...Config to distinguish it
// from the runtime docker container name the CLI works with.
func (f *dbFeature) sqlServerContainerConfig() (*config.Container, bool) {
	return SQLServerContainer(f.host.Config())
}

// dbWorkflowConfigured reports whether the active env opts into the DB
// workflow. Derived, not a setting: envs that use the DB workflow all
// define sql-server, so they behave exactly as before the gate existed.
func (f *dbFeature) dbWorkflowConfigured() bool {
	return DBWorkflowConfigured(f.host.Config())
}

// rejectIfDBNotConfigured writes the standard not-configured error and
// reports whether the request was rejected.
func (f *dbFeature) rejectIfDBNotConfigured(w http.ResponseWriter) bool {
	if f.dbWorkflowConfigured() {
		return false
	}
	daemon.WriteJSON(w, http.StatusPreconditionFailed, daemon.APIResponse{Error: ErrMsgDBNotConfigured})
	return true
}

// DBWorkflowSkippedCheck is the informational entry both doctor surfaces
// (daemon runDoctorChecks and the CLI daemon-down fallback) emit when the
// active env has no sql-server container. One constructor so the wording
// can't drift between the daemon-up and daemon-down paths.
func DBWorkflowSkippedCheck() daemon.DoctorCheck {
	return daemon.DoctorCheck{Name: "DB Workflow", Status: daemon.CheckInfo, Message: "not configured (no sql-server container or sql_projects in the active env) — db checks skipped"}
}

// SQLServerContainerName is the container name that opts an env into the
// DB workflow. Exported so consumers (snapshots, doctor, CLI) reference
// the concept instead of repeating the literal. Owned by the DB-workflow
// gate domain — the core config schema no longer knows the convention
// (spec B5/B6).
const SQLServerContainerName = "sql-server"

// SQLServerContainer returns the env's sql-server container. Single owner
// of the DB-workflow gate predicate (the container name and the
// present-but-nil rule live here and nowhere else). Callers must hold a
// loaded config — doctor branches on nil before calling because "config
// failed to load" needs different output than "not configured".
func SQLServerContainer(cfg *config.Config) (*config.Container, bool) {
	sql := cfg.Containers[SQLServerContainerName]
	return sql, sql != nil
}

// DBWorkflowConfigured reports whether this env opts into the DB
// workflow: a sql-server container (the legacy rebuild/reset
// convention) or a declared sql_projects publish target. The gate must
// accept every env publishTarget can resolve — a generic env whose
// target isn't named sql-server would otherwise be rejected before
// target resolution ever runs.
func DBWorkflowConfigured(cfg *config.Config) bool {
	if _, ok := SQLServerContainer(cfg); ok {
		return true
	}
	if sp := cfg.SQLProjects; sp != nil {
		return cfg.Containers[sp.Target] != nil
	}
	return false
}
