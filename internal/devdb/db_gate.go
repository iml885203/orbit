// The SQL Server workflow gate. Environments opt in explicitly with a
// sqlserver section; container names and image strings never enable features.

package devdb

import (
	"net/http"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

// ErrMsgDBNotConfigured is returned by every DB-workflow endpoint (and
// reused verbatim by the CLI) when the active env doesn't opt in.
const ErrMsgDBNotConfigured = "SQL Server workflow not configured: add a sqlserver section to the active environment"

// sqlServerContainerConfig returns the workflow's explicit target container.
func (f *dbFeature) sqlServerContainerConfig() (*config.Container, bool) {
	return SQLServerContainer(f.host.Config())
}

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

// SQLServerContainer returns the target declared by the sqlserver section.
func SQLServerContainer(cfg *config.Config) (*config.Container, bool) {
	section := SQLServerFrom(cfg)
	if section == nil || section.Target == "" {
		return nil, false
	}
	target := cfg.Containers[section.Target]
	return target, target != nil
}

func DBWorkflowConfigured(cfg *config.Config) bool {
	return SQLServerFrom(cfg) != nil
}
