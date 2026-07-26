package sqlpublish

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeProj lays down a minimal .sqlproj + one .sql file under a fresh
// dir and returns the .sqlproj path.
func writeProj(t *testing.T, sql string) string {
	t.Helper()
	dir := t.TempDir()
	proj := filepath.Join(dir, "App.sqlproj")
	if err := os.WriteFile(proj, []byte("<Project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Table.sql"), []byte(sql), 0o644); err != nil {
		t.Fatal(err)
	}
	return proj
}

func TestProjectFingerprint_StableWhenUnchanged(t *testing.T) {
	proj := writeProj(t, "CREATE TABLE X (id INT)")
	a, err := projectFingerprint(proj, "AppDB")
	if err != nil {
		t.Fatal(err)
	}
	b, err := projectFingerprint(proj, "AppDB")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("fingerprint must be stable for unchanged source: %s != %s", a, b)
	}
}

func TestProjectFingerprint_ChangesWithSource(t *testing.T) {
	proj := writeProj(t, "CREATE TABLE X (id INT)")
	before, err := projectFingerprint(proj, "AppDB")
	if err != nil {
		t.Fatal(err)
	}

	// Rewrite the .sql with different content and a bumped mtime.
	sqlFile := filepath.Join(filepath.Dir(proj), "Table.sql")
	if err := os.WriteFile(sqlFile, []byte("CREATE TABLE X (id BIGINT)"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(sqlFile, future, future); err != nil {
		t.Fatal(err)
	}

	after, err := projectFingerprint(proj, "AppDB")
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Error("fingerprint must change when a source file changes")
	}
}

func TestProjectFingerprint_ChangesWithDBName(t *testing.T) {
	proj := writeProj(t, "CREATE TABLE X (id INT)")
	a, _ := projectFingerprint(proj, "AppDB")
	b, _ := projectFingerprint(proj, "OtherDB")
	if a == b {
		t.Error("fingerprint must differ per target DB name")
	}
}

// A change in a referenced (shared) project must invalidate the leaf
// project's fingerprint — the reference contributes objects to the built
// dacpac, so a stale cache there would serve the wrong schema. This is the
// regression test for the CommonFiles-reference bug found in local testing.
func TestProjectFingerprint_FollowsProjectReferences(t *testing.T) {
	root := t.TempDir()
	// Shared project: root/CommonFiles/CommonFiles.sqlproj + Login.sql
	commonDir := filepath.Join(root, "CommonFiles")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(commonDir, "CommonFiles.sqlproj"), []byte("<Project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	login := filepath.Join(commonDir, "Login.sql")
	if err := os.WriteFile(login, []byte("CREATE ROLE app"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Leaf project references it (MSBuild-style backslash relative path).
	leafDir := filepath.Join(root, "App")
	if err := os.MkdirAll(leafDir, 0o755); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(leafDir, "App.sqlproj")
	proj := `<Project><ItemGroup><ProjectReference Include="..\CommonFiles\CommonFiles.sqlproj" /></ItemGroup></Project>`
	if err := os.WriteFile(leaf, []byte(proj), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leafDir, "Table.sql"), []byte("CREATE TABLE X (id INT)"), 0o644); err != nil {
		t.Fatal(err)
	}

	before, err := projectFingerprint(leaf, "AppDB")
	if err != nil {
		t.Fatal(err)
	}

	// Edit only the SHARED project's source + bump its mtime.
	if err := os.WriteFile(login, []byte("CREATE ROLE app2"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(login, future, future); err != nil {
		t.Fatal(err)
	}

	after, err := projectFingerprint(leaf, "AppDB")
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Error("editing a referenced project's source must change the leaf fingerprint")
	}
}

func TestProjectFingerprint_IgnoresBinObj(t *testing.T) {
	proj := writeProj(t, "CREATE TABLE X (id INT)")
	before, _ := projectFingerprint(proj, "AppDB")

	// A prior build's artifacts under bin/ must not perturb the fingerprint.
	binDir := filepath.Join(filepath.Dir(proj), "bin", "Debug")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "AppDB.dacpac"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	after, _ := projectFingerprint(proj, "AppDB")
	if before != after {
		t.Error("fingerprint must ignore bin/ and obj/ build output")
	}
}
