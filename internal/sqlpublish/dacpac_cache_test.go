package sqlpublish

import (
	"os"
	"path/filepath"
	"testing"
)

// A build that emits both a leaf dacpac and a referenced dacpac must cache
// BOTH: restoring only the leaf breaks sqlpackage DeployReport/Publish,
// which needs the referenced dacpac beside it to resolve external objects.
// Regression test for the ApplicationSetting/CommonFiles.dacpac failure.
func TestCacheRoundTrip_RestoresAllDacpacs(t *testing.T) {
	buildOut := t.TempDir()
	for _, name := range []string{"AppDB.dacpac", "CommonFiles.dacpac"} {
		if err := os.WriteFile(filepath.Join(buildOut, name), []byte("dac:"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A non-dacpac artifact must not be cached.
	if err := os.WriteFile(filepath.Join(buildOut, "AppDB.dll"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	cacheDir := filepath.Join(t.TempDir(), "fp")
	if err := storeDacpacs(buildOut, cacheDir); err != nil {
		t.Fatalf("store: %v", err)
	}

	restoreInto := t.TempDir()
	n, err := restoreDacpacs(cacheDir, restoreInto)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if n != 2 {
		t.Errorf("want 2 dacpacs restored (leaf + referenced); got %d", n)
	}
	for _, name := range []string{"AppDB.dacpac", "CommonFiles.dacpac"} {
		if _, err := os.Stat(filepath.Join(restoreInto, name)); err != nil {
			t.Errorf("%s not restored: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(restoreInto, "AppDB.dll")); err == nil {
		t.Error("non-dacpac artifact must not be cached/restored")
	}
}

func TestRestoreDacpacs_RejectsIncompleteCache(t *testing.T) {
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cacheDir, "AppDB.dacpac"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := restoreDacpacs(cacheDir, t.TempDir())
	if err == nil || n != 0 {
		t.Fatalf("incomplete cache restored: n=%d err=%v", n, err)
	}
}
