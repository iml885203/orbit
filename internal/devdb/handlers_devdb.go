package devdb

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/tunnel"
)

func (f *dbFeature) handleDevDBProjects(w http.ResponseWriter, r *http.Request) {
	if daemon.RequireMethod(w, r, http.MethodGet) {
		return
	}
	if f.rejectIfDBNotConfigured(w) {
		return
	}

	workspaceRoot := f.workspaceRoot()
	if workspaceRoot == "" {
		daemon.WriteJSON(w, http.StatusInternalServerError, daemon.APIResponse{Error: errWorkspaceRootUnavailable.Error()})
		return
	}

	projects, err := f.allProjects()
	if err != nil {
		daemon.WriteJSON(w, http.StatusInternalServerError, daemon.APIResponse{Error: err.Error()})
		return
	}
	daemon.WriteJSON(w, http.StatusOK, DevDBProjectsResponse{Projects: projects})
}

func (f *dbFeature) handleDevDBMeta(w http.ResponseWriter, r *http.Request) {
	if daemon.RequireMethod(w, r, http.MethodGet) {
		return
	}

	sqlImage := ""
	if publishTarget, _, ok := f.publishTarget(); ok {
		sqlImage = publishTarget.Image
	}
	configured := f.dbWorkflowConfigured()
	// If settings override the image, show that instead
	if img := f.host.Settings().Get("sql_server_image"); img != "" {
		sqlImage = img
	}

	configPath := f.host.ConfigPath()
	workspaceRoot := f.workspaceRoot()
	if workspaceRoot == "" {
		workspaceRoot = "unknown"
	}

	claimConfigured := tunnel.ClaimFrom(f.host.Config()) != nil

	sqlPort := 0
	sqlTarget := ""
	if c, targetName, ok := f.publishTarget(); ok {
		if p, err := publishTargetHostPort(c); err == nil {
			sqlPort = p
		}
		sqlTarget = dbTargetDockerName(targetName)
	}

	daemon.WriteJSON(w, http.StatusOK, DevDBMetaResponse{
		EnvironmentPath: configPath,
		EnvironmentName: filepath.Base(configPath),
		SQLServerImage:  sqlImage,
		SQLServerPort:   sqlPort,
		SQLServerTarget: sqlTarget,
		WorkspaceRoot:   workspaceRoot,
		DBConfigured:    &configured,
		ClaimConfigured: &claimConfigured,
	})
}

func (f *dbFeature) workspaceRoot() string {
	if root := daemon.WorkspaceRootFromEnv(); root != "" {
		return root
	}
	// Fallback: derive from layout <workspaceRoot>/orbit/envs/<env>.yaml —
	// i.e. configPath's great-grandparent. Used when orbit is run from a
	// checkout without a workspace root configured.
	if configPath := f.host.ConfigPath(); configPath != "" {
		return filepath.Dir(filepath.Dir(filepath.Dir(configPath)))
	}
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Dir(cwd)
	}
	return ""
}

// dbRoot returns an explicit override for the directory that holds
// SQL project subdirectories. Empty when unset — callers fall back to the standard
// <workspaceRoot>[/dbprojects] scan locations. Resolution order: ORBIT_DB_ROOT
// env var, then the `db_root` setting.
func (f *dbFeature) dbRoot() string {
	return resolveDBRootPath(f.host.Settings())
}

// resolveDBRootPath is the shared resolution for the db-projects
// root override, so every caller (daemon meta, doctor, init) agrees:
// ORBIT_DB_ROOT env, then the persisted db_root setting.
func resolveDBRootPath(s *daemon.Settings) string {
	if root := os.Getenv("ORBIT_DB_ROOT"); root != "" {
		return root
	}
	return s.Get("db_root")
}
