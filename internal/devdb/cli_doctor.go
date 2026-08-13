package devdb

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
//
// It shares the workspace-root, sqlProjectChecks, sqlServerReadinessChecks
// and publishToolchainChecks entries with the daemon's dbWorkflowChecks
// (doctor_checks.go), and a new check that reads only config belongs in both.
//
// Deliberately absent here: sqlImageChecks. It hangs off *dbFeature because
// it inspects local images and registry access through the daemon's host,
// which the offline CLI does not have. The two lists differing on that one
// entry is correct, not drift — worth stating, because "the CLI twin is
// missing a check" reads like a defect to anyone who compares them.
func CLIDoctorChecks(cfg *config.Config) []daemon.DoctorCheck {
	if !DBWorkflowConfigured(cfg) {
		return nil
	}
	root := daemon.WorkspaceRootFromEnv()
	rootCheck, _ := daemon.WorkspaceRootCheck(root)
	checks := make([]daemon.DoctorCheck, 0, 4)
	checks = append(checks, rootCheck)
	checks = append(checks, sqlProjectChecks(cfg, root)...)
	checks = append(checks, sqlServerReadinessChecks(cfg)...)
	checks = append(checks, publishToolchainChecks()...)
	return checks
}

// PrintDBWorkflowChecks is the human-rendered twin of the daemon's
// dbWorkflowChecks. Envs without this optional workflow stay silent.
func PrintDBWorkflowChecks(cfg *config.Config) {
	pass := cli.Green.Sprint("✓")
	fail := cli.Red.Sprint("✗")
	warn := cli.Yellow.Sprint("!")

	if !DBWorkflowConfigured(cfg) {
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
	for _, check := range sqlServerReadinessChecks(cfg) {
		fmt.Printf("  %s %s: %s\n", warn, check.Name, check.Message)
		fmt.Printf("      hint: %s\n", check.Hint)
	}

	containerMgr, containerErr := container.NewManager(os.Getenv("ORBIT_NAMESPACE"))
	if containerErr != nil {
		return
	}
	defer func() { _ = containerMgr.Close() }()

	// The target name and image are arbitrary; only the explicit section
	// identifies the SQL Server workflow.
	c, ok := SQLServerContainer(cfg)
	if !ok {
		return
	}
	if containerMgr.ImageExists(c.Image) {
		fmt.Printf("  %s SQL image available locally\n", pass)
	} else {
		fmt.Printf("  %s SQL image not cached — will be pulled on first orbit up\n", warn)
	}
}
