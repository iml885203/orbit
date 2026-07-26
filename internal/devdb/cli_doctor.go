package devdb

// The offline `orbit doctor` contribution — moved from cmd/orbit when
// the neutral CLI became core-bound (repo-split S1b). The daemon-running
// path contributes through DoctorRegistrar in daemonSetup instead.

import (
	"fmt"
	"os"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/container"
	"github.com/iml885203/orbit/daemon"
)

// CLIDoctorChecks is the structured (--json) twin of the human printer:
// the DB-workflow gate applied to the offline local doctor response.
func CLIDoctorChecks(cfg *config.Config) []daemon.DoctorCheck {
	if !DBWorkflowConfigured(cfg) {
		return []daemon.DoctorCheck{DBWorkflowSkippedCheck()}
	}
	rootCheck, _ := daemon.WorkspaceRootCheck(daemon.WorkspaceRootFromEnv())
	return []daemon.DoctorCheck{rootCheck}
}

// PrintDBWorkflowChecks is the human-rendered twin of the daemon's
// dbWorkflowChecks: workspace root, the optional db-root override, SQL
// image presence, and the db build repos. Envs without a
// sql-server container get one skip line instead of red noise.
func PrintDBWorkflowChecks(cfg *config.Config) {
	pass := cli.Green.Sprint("✓")
	fail := cli.Red.Sprint("✗")

	if !DBWorkflowConfigured(cfg) {
		fmt.Printf("  %s %s\n", cli.Faint.Sprint("—"), DBWorkflowSkippedCheck().Message)
		return
	}

	workspaceRoot := daemon.WorkspaceRootFromEnv()
	if workspaceRoot == "" {
		fmt.Printf("  %s workspace root not set (WORKSPACE_ROOT or 'orbit init')\n", cli.Yellow.Sprint("!"))
	} else if _, err := os.Stat(workspaceRoot); err != nil {
		fmt.Printf("  %s workspace root %s (path not found)\n", fail, workspaceRoot)
	} else {
		fmt.Printf("  %s workspace root %s\n", pass, workspaceRoot)
	}

	// DB projects root (optional override). Resolved via the same 3-tier
	// precedence as the daemon so offline and online doctor agree, and only
	// reported when set (ORBIT_DB_ROOT / legacy env / db_root setting).
	if dbRoot := resolveDBRootPath(daemon.LoadSettings(daemon.DefaultSettingsPath())); dbRoot != "" {
		if _, err := os.Stat(dbRoot); err != nil {
			fmt.Printf("  %s DB root %s (path not found)\n", fail, dbRoot)
		} else {
			fmt.Printf("  %s DB root %s\n", pass, dbRoot)
		}
	}

	containerMgr, containerErr := container.NewManager(os.Getenv("ORBIT_NAMESPACE"))
	if containerErr != nil {
		return
	}
	defer func() { _ = containerMgr.Close() }()

	// The gate also passes generic sql_projects envs whose target container
	// isn't named sql-server, so a passing gate doesn't guarantee one here —
	// nothing to report on the image in that case (mirrors sqlImageChecks).
	c, ok := SQLServerContainer(cfg)
	if !ok {
		return
	}
	if containerMgr.ImageExists(c.Image) {
		fmt.Printf("  %s SQL image available locally\n", pass)
	} else {
		fmt.Printf("  %s SQL image not cached — will be pulled on first orbit up\n", cli.Yellow.Sprint("!"))
	}
}
