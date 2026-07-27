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
	configPath := f.host.ConfigPath()
	workspaceRoot := f.workspaceRoot()
	if workspaceRoot == "" {
		workspaceRoot = "unknown"
	}

	claimConfigured := tunnel.ClaimFrom(f.host.Config()) != nil

	sqlPort := 0
	sqlTarget := ""
	sqlService := ""
	sqlUsername := ""
	sqlPasswordEnv := ""
	if c, targetName, ok := f.publishTarget(); ok {
		if p, err := publishTargetHostPort(c); err == nil {
			sqlPort = p
		}
		sqlTarget = dbTargetDockerName(targetName)
		sqlService = targetName
	}
	if section := SQLServerFrom(f.host.Config()); section != nil {
		sqlUsername = section.Username
		sqlPasswordEnv = section.PasswordEnv
	}

	daemon.WriteJSON(w, http.StatusOK, DevDBMetaResponse{
		EnvironmentPath:      configPath,
		EnvironmentName:      filepath.Base(configPath),
		SQLServerImage:       sqlImage,
		SQLServerPort:        sqlPort,
		SQLServerTarget:      sqlTarget,
		SQLServerService:     sqlService,
		SQLServerUsername:    sqlUsername,
		SQLServerPasswordEnv: sqlPasswordEnv,
		WorkspaceRoot:        workspaceRoot,
		DBConfigured:         &configured,
		ClaimConfigured:      &claimConfigured,
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
