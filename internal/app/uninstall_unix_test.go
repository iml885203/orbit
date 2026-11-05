//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveUninstallArtifactsRemovesBinaryAndSelectedData(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "bin", "orbit")
	home := filepath.Join(root, "orbit-home")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary+".prev", []byte("previous"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := uninstallArtifacts(binary, home, true)
	scheduled, err := removeUninstallArtifacts(paths)
	if err != nil {
		t.Fatal(err)
	}
	if scheduled {
		t.Fatal("Unix removal was scheduled instead of completed")
	}
	for _, path := range []string{binary, binary + ".prev", home} {
		if pathExists(path) {
			t.Errorf("%s still exists after removal", path)
		}
	}
}
