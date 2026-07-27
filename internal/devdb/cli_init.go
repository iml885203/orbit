package devdb

// The `orbit init` contribution — the DB workflow settings steps and
// db-root detection, moved from the neutral wizard when the CLI became
// core-bound (repo-split S1b/S2 prep), then split from the brand
// workspace-candidate hints when the DB workflow dissolved into a
// neutral package (repo-split S25).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

// InitSteps runs the DB workflow settings prompts (init wizard step 1b):
// the db-projects root. Optional; a blank answer skips the write.
func InitSteps(settings *daemon.Settings, yes bool, prompt func(string) string, quiet bool) error {
	// ORBIT_DB_ROOT — directory containing SQL project subdirectories.
	// Optional; if left blank, devdb falls back to scanning
	// <workspace_root> and <workspace_root>/dbprojects.
	dbRoot := detectDBRoot(settings)
	if !yes {
		promptLabel := "  ORBIT_DB_ROOT (blank to skip): "
		if dbRoot != "" {
			promptLabel = fmt.Sprintf("  ORBIT_DB_ROOT [%s]: ", dbRoot)
		}
		if input := prompt(promptLabel); input != "" {
			dbRoot = initExpandHome(input)
		}
	}
	if dbRoot != "" {
		if !quiet {
			if _, err := os.Stat(dbRoot); err != nil {
				_, _ = cli.Yellow.Printf("  ! ORBIT_DB_ROOT=%s (path not found — saved anyway)\n", dbRoot)
			} else if projects := findSQLProjectDirs(dbRoot); len(projects) > 0 {
				fmt.Printf("  %s ORBIT_DB_ROOT=%s (contains %s)\n", cli.Green.Sprint("✓"), dbRoot, strings.Join(projects, ", "))
			} else {
				fmt.Printf("  %s ORBIT_DB_ROOT=%s (no SQL projects found)\n", cli.Yellow.Sprint("!"), dbRoot)
			}
		}
		if err := settings.Set("db_root", dbRoot); err != nil {
			return fmt.Errorf("saving settings: %w", err)
		}
		settings.ApplyToEnv()
	}

	return nil
}

// detectDBRoot resolves the db-projects root shown as the prompt default,
// via the shared 3-tier precedence (ORBIT_DB_ROOT env, legacy env, then
// the db_root setting). Returns "" when none is set — the user types it.
func detectDBRoot(settings *daemon.Settings) string {
	return resolveDBRootPath(settings)
}

// initExpandHome mirrors the wizard's ~/ expansion for typed paths.
func initExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
