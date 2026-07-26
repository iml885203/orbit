package devdb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/config"
)

// writeProjFixture creates <root>/<project>/<db>/<db>.sqlproj — the
// layout the allowlist scan and the .sqlproj lookup both key off.
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

// allProjects publishes exactly the team-shared allowlist (db_projects),
// located under the workspace by CASE-INSENSITIVE folder name — so a
// project cased differently on disk than in the shared list still
// resolves, and a folder the list never named (another team's project,
// or anything else checked out beside it) is never swept in.
func TestAllProjects_Allowlist(t *testing.T) {
	workspace := t.TempDir()
	writeProjFixture(t, workspace, "DBProject.Payment", "PaymentDB") // cased differently than the list
	writeProjFixture(t, workspace, "dbproject.account", "AccountingDB")
	writeProjFixture(t, workspace, "artemis", "ArtemisDB") // not on the allowlist
	t.Setenv("WORKSPACE_ROOT", workspace)

	t.Run("allowlist matches case-insensitively and excludes the rest", func(t *testing.T) {
		cfg := (&config.Config{}).WithExtension("db_projects", &DBProjectsConfig{
			Projects: []string{"dbproject.payment", "dbproject.account"},
		})
		f := newTestDBFeature(t, cfg)
		projects, err := f.allProjects()
		if err != nil {
			t.Fatalf("allProjects: %v", err)
		}
		names := projectNames(projects)
		if !names["DBProject.Payment"] {
			t.Errorf("lowercase allowlist entry must match the DBProject.Payment folder; got %v", names)
		}
		if !names["dbproject.account"] {
			t.Errorf("dbproject.account is allowlisted; got %v", names)
		}
		if names["artemis"] {
			t.Errorf("artemis is not allowlisted and must be excluded; got %v", names)
		}
	})

	t.Run("no allowlist means no projects", func(t *testing.T) {
		f := newTestDBFeature(t, &config.Config{})
		projects, err := f.allProjects()
		if err != nil {
			t.Fatalf("allProjects: %v", err)
		}
		if len(projects) != 0 {
			t.Errorf("without an allowlist the workflow publishes nothing; got %v", projectNames(projects))
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
		{Name: "wallet", Path: filepath.Join(root, "wallet"), Databases: []string{"WalletDB"}},
		{Name: "game", Path: filepath.Join(root, "game"), Databases: []string{"GameDB"}},
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
