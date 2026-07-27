package devdb

import (
	"log/slog"
	"net/http"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/orbit/internal/dbstate"
)

// SetupDaemon wires the DB workflow (publish/snapshot, db-state) into
// the daemon: construction of all feature state moved here from
// NewServer when the feature became extension-owned (spec B6), then
// split from the tunnel half when the DB workflow dissolved into a
// neutral package (repo-split S25). Runs once during route setup,
// before any listener serves.
func SetupDaemon(host extension.Host, mux *http.ServeMux) extension.DaemonHooks {
	hooks := extension.DaemonHooks{}

	dh, ok := host.(daemonHost)
	if !ok {
		panic("devdb SetupDaemon: host does not provide the daemon capabilities")
	}
	db := &dbFeature{host: dh, dbOps: newDBOpsManager(), drift: newDriftCache()}
	if ds, err := dbstate.New(daemon.OrbitDir()); err != nil {
		slog.Error("dbstate store unavailable", "component", "daemon", "err", err)
	} else {
		db.dbState = ds
		registerDBStateHandlers(mux, db)
		hooks.EventSources = append(hooks.EventSources,
			extension.EventSource{Name: "dbstate", Run: extension.RunChannel(ds.Subscribe)})
	}
	registerDBPublishHandler(mux, db)
	registerDBResetHandler(mux, db)
	registerDBDiffHandler(mux, db)
	mux.HandleFunc("/api/devdb/projects", db.handleDevDBProjects)
	mux.HandleFunc("/api/devdb/meta", db.handleDevDBMeta)
	// dbstate before dbop preserves the pre-registry launch order (subscribes
	// enqueue initial frames). Any later source another feature contributes —
	// e.g. tunnel-access — is appended after these by the composition root's
	// hook merge, keeping that same relative order.
	hooks.EventSources = append(hooks.EventSources,
		extension.EventSource{Name: "dbop", Run: extension.RunChannel(db.dbOps.Subscribe)},
	)
	if rr, ok := host.(daemon.ResourceRegistrar); ok {
		rr.AddResourceContributor(db.dbResources)
	}
	if dr, ok := host.(daemon.DoctorRegistrar); ok {
		dr.AddDoctorChecks(db.dbWorkflowChecks)
	}
	return hooks
}
