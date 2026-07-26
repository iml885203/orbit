package devdb

import (
	"os"
	"path/filepath"
	"testing"
)

// listAllowedProjects returns exactly the allowlisted (case-folded)
// directories that carry the <project>/*/*.sqlproj layout — never a
// folder the list didn't name, and never one without the layout.
func TestListAllowedProjects_AllowlistAndLayout(t *testing.T) {
	root := t.TempDir()
	writeProjFixture(t, root, "DBProject.Legacy", "LegacyDB") // cased differently than the allowlist
	writeProjFixture(t, root, "artemis", "OrdersDB")          // has the layout but is not allowlisted
	// Allowlisted name but NO *.sqlproj — must not appear.
	if err := os.MkdirAll(filepath.Join(root, "dbproject.empty", "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	allow := map[string]bool{"dbproject.legacy": true, "dbproject.empty": true}
	projects, err := listAllowedProjects(root, "", allow)
	if err != nil {
		t.Fatalf("listAllowedProjects: %v", err)
	}
	got := projectNames(projects)
	if !got["DBProject.Legacy"] {
		t.Errorf("case-folded allowlist must match the DBProject.Legacy folder; got %v", got)
	}
	if got["artemis"] {
		t.Errorf("artemis is not allowlisted; got %v", got)
	}
	if got["dbproject.empty"] {
		t.Errorf("allowlisted but no *.sqlproj — must not appear; got %v", got)
	}
}

// The db-root override is prepended to the scan roots, so an allowlisted
// project living only there resolves too.
func TestListAllowedProjects_HonorsDBRootOverride(t *testing.T) {
	workspace := t.TempDir()
	dbRoot := t.TempDir()
	writeProjFixture(t, dbRoot, "billing", "BillingDB")

	projects, err := listAllowedProjects(workspace, dbRoot, map[string]bool{"billing": true})
	if err != nil {
		t.Fatalf("listAllowedProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "billing" {
		t.Fatalf("expected billing under the db-root override; got %+v", projects)
	}
	if len(projects[0].Databases) != 1 || projects[0].Databases[0] != "BillingDB" {
		t.Errorf("expected [BillingDB]; got %v", projects[0].Databases)
	}
}
