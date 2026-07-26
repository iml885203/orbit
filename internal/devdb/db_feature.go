package devdb

import (
	"context"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/orbit/internal/dbstate"
	"github.com/iml885203/orbit/internal/sqlpublish"
)

// daemonHost is the full surface the DB feature consumes: the extension
// contract plus the daemon-typed capabilities it asserts at setup time.
type daemonHost interface {
	extension.Host
	Settings() *daemon.Settings
	Containers() daemon.ContainerOps
	Restarter() daemon.ServiceRestarter
	BaseContext() context.Context
	ConfigPath() string
	ResolveWorkspaceRoot() (string, daemon.DoctorCheck, bool)
}

// dbFeature owns the DB workflow's daemon state: the db-state store,
// the publish op serializer, and the drift cache — fields that used to
// live on daemon.Server.
type dbFeature struct {
	host       daemonHost
	dbState    *dbstate.Store
	dbOps      *dbOpsManager
	drift      *driftCache
	diffRunner func(sqlpublish.Opts) (sqlpublish.DiffResult, sqlpublish.ErrorCode, error)
}
