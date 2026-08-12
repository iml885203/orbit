package devdb

import (
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/config"
)

// A source added with `--path <env repo> --workspace <app checkout>` keeps the
// environment YAML and the .sqlproj trees in separate places. Project paths
// resolve against the workspace, never against the directory the config was
// loaded from — otherwise every declared .sqlproj would be looked up under the
// env repo, where none of them exist.
func TestAllProjects_ResolvesAgainstWorkspaceNotConfigLocation(t *testing.T) {
	workspace := t.TempDir() // the app checkout: where .sqlproj files live
	envRepo := t.TempDir()   // the e2e worktree: where the YAML lives
	writeProjFixture(t, workspace, "schema.platform", "PlatformDB")

	cfg := (&config.Config{}).WithExtension(sqlServerSection, &SQLServerConfig{
		Projects: []SQLServerProjectConfig{
			{
				Path:      "schema.platform/PlatformDB/PlatformDB.sqlproj",
				Databases: []string{"PlatformDB_dev", "PlatformDB_e2e"},
			},
		},
	})
	t.Setenv("WORKSPACE_ROOT", workspace)
	f := newTestDBFeature(t, cfg)
	f.host.(*fakeDaemonHost).configPath = filepath.Join(envRepo, "envs", "backoffice.yaml")

	projects, err := f.allProjects()
	if err != nil {
		t.Fatalf("allProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("want 1 project, got %d", len(projects))
	}

	want := filepath.Join(workspace, "schema.platform/PlatformDB/PlatformDB.sqlproj")
	if projects[0].Path != want {
		t.Errorf("project path = %q, want it under the workspace at %q", projects[0].Path, want)
	}

	// Both declared names must reach the same project file — the point of
	// declaring several databases on one .sqlproj.
	for _, db := range []string{"PlatformDB_dev", "PlatformDB_e2e"} {
		proj, err := sqlProjForDatabaseOrError(projects, db)
		if err != nil {
			t.Errorf("%s: %v", db, err)
			continue
		}
		if proj != want {
			t.Errorf("%s resolved to %q, want %q", db, proj, want)
		}
	}
}
