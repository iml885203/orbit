package devdb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/config"
)

// writeProjFixture creates <root>/<project>/<db>/<db>.sqlproj.
func writeProjFixture(t *testing.T, root, project, db string) {
	t.Helper()
	dir := filepath.Join(root, project, db)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, db+".sqlproj"), []byte("<Project/>"), 0o644); err != nil {
		t.Fatalf("write sqlproj: %v", err)
	}
}

func projectNames(projects []DevDBProject) map[string]bool {
	names := map[string]bool{}
	for _, p := range projects {
		names[p.Name] = true
	}
	return names
}

func TestAllProjects_ExplicitPaths(t *testing.T) {
	workspace := t.TempDir()
	writeProjFixture(t, workspace, "DBProject.Payment", "PaymentDB") // cased differently than the list
	writeProjFixture(t, workspace, "dbproject.account", "AccountingDB")
	writeProjFixture(t, workspace, "artemis", "ArtemisDB") // not explicitly configured
	t.Setenv("WORKSPACE_ROOT", workspace)

	t.Run("declared directories are included and unrelated directories are excluded", func(t *testing.T) {
		cfg := (&config.Config{}).WithExtension(sqlServerSection, &SQLServerConfig{
			Projects: []SQLServerProjectConfig{
				{Path: "DBProject.Payment/PaymentDB/PaymentDB.sqlproj"},
				{Path: "dbproject.account/AccountingDB/AccountingDB.sqlproj"},
			},
		})
		f := newTestDBFeature(t, cfg)
		projects, err := f.allProjects()
		if err != nil {
			t.Fatalf("allProjects: %v", err)
		}
		names := projectNames(projects)
		if !names["PaymentDB"] {
			t.Errorf("declared PaymentDB project is missing; got %v", names)
		}
		if !names["AccountingDB"] {
			t.Errorf("declared AccountingDB project is missing; got %v", names)
		}
		if names["artemis"] {
			t.Errorf("undeclared artemis folder must be excluded; got %v", names)
		}
	})

	t.Run("no explicit project list means no projects", func(t *testing.T) {
		f := newTestDBFeature(t, &config.Config{})
		projects, err := f.allProjects()
		if err != nil {
			t.Fatalf("allProjects: %v", err)
		}
		if len(projects) != 0 {
			t.Errorf("without sqlserver.projects the workflow publishes nothing; got %v", projectNames(projects))
		}
	})

	t.Run("one project can target several named databases", func(t *testing.T) {
		cfg := (&config.Config{}).WithExtension(sqlServerSection, &SQLServerConfig{
			Projects: []SQLServerProjectConfig{{
				Path:      "DBProject.Payment/PaymentDB/PaymentDB.sqlproj",
				Databases: []string{"PaymentDev", "PaymentE2E"},
			}},
		})
		projects, err := newTestDBFeature(t, cfg).allProjects()
		if err != nil {
			t.Fatalf("allProjects: %v", err)
		}
		if len(projects) != 1 || len(projects[0].Databases) != 2 ||
			projects[0].Databases[0] != "PaymentDev" || projects[0].Databases[1] != "PaymentE2E" {
			t.Fatalf("projects = %+v", projects)
		}
	})
}

// publishTargetsForDBs feeds both `publish --all` and a single project's
// fan-out. Its two behaviours are easy to break on refactor and untested:
// first-occurrence-wins dedup, and silently skipping a db whose .sqlproj
// can't be resolved (never erroring). Uses the same on-disk fixture layout
// the lookup keys off.
func TestPublishTargetsForDBs(t *testing.T) {
	root := t.TempDir()
	writeProjFixture(t, root, "wallet", "WalletDB")
	writeProjFixture(t, root, "game", "GameDB")
	projects := []DevDBProject{
		{Name: "WalletDB", Path: filepath.Join(root, "wallet", "WalletDB", "WalletDB.sqlproj"), Databases: []string{"WalletDB"}},
		{Name: "GameDB", Path: filepath.Join(root, "game", "GameDB", "GameDB.sqlproj"), Databases: []string{"GameDB"}},
	}

	t.Run("dedups repeated db names, first occurrence wins", func(t *testing.T) {
		got := publishTargetsForDBs(projects, []string{"WalletDB", "GameDB", "WalletDB"})
		if len(got) != 2 {
			t.Fatalf("want 2 targets after dedup; got %d (%v)", len(got), got)
		}
		if got[0].DB != "WalletDB" || got[1].DB != "GameDB" {
			t.Errorf("order must follow first occurrence; got %v", got)
		}
	})

	t.Run("skips a db with no resolvable .sqlproj rather than erroring", func(t *testing.T) {
		got := publishTargetsForDBs(projects, []string{"WalletDB", "GhostDB"})
		if len(got) != 1 || got[0].DB != "WalletDB" {
			t.Errorf("an unresolvable db must be skipped, the rest kept; got %v", got)
		}
	})

	t.Run("each target carries its resolved .sqlproj path", func(t *testing.T) {
		got := publishTargetsForDBs(projects, []string{"WalletDB"})
		if len(got) != 1 || got[0].SQLProj == "" {
			t.Errorf("target must carry a non-empty .sqlproj path; got %v", got)
		}
	})
}
