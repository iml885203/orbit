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
	root, _ := workspaceRootFor(f.host.ConfigPath())
	return root
}

// workspaceRootFor is the root SQL project paths are joined to. derived is
// true when WORKSPACE_ROOT is unset and the root was inferred instead, so the
// doctor can report the value publish and the drift check actually use rather
// than calling it unset.
func workspaceRootFor(configPath string) (root string, derived bool) {
	if root := daemon.WorkspaceRootFromEnv(); root != "" {
		return root, false
	}
	// Fallback: derive from layout <workspaceRoot>/orbit/envs/<env>.yaml —
	// i.e. configPath's great-grandparent. Used when orbit is run from a
	// checkout without a workspace root configured.
	if configPath != "" {
		return filepath.Dir(filepath.Dir(filepath.Dir(configPath))), true
	}
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Dir(cwd), true
	}
	return "", false
}

func workspaceRootCheck(configPath string) daemon.DoctorCheck {
	root, derived := workspaceRootFor(configPath)
	check, _ := daemon.WorkspaceRootCheck(root)
	if derived && check.Status == daemon.CheckPass {
		check.Message = root + " (derived from the config location; set WORKSPACE_ROOT to override)"
	}
	return check
}
