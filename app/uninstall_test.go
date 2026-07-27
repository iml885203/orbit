package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUninstallArtifactsPreserveUserDataByDefault(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "bin", "orbit")
	home := filepath.Join(t.TempDir(), "orbit-home")
	got := uninstallArtifacts(binary, home, false)
	for _, path := range got {
		if path == home {
			t.Fatalf("default uninstall artifacts include user data: %v", got)
		}
	}
}

func TestUninstallArtifactsIncludeUserDataOnlyWithPurge(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "bin", "orbit")
	home := filepath.Join(t.TempDir(), "orbit-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	got := uninstallArtifacts(binary, home, true)
	if got[len(got)-1] != home {
		t.Fatalf("purge artifacts = %v, want user data last", got)
	}
}

func TestValidatePurgeTargetRejectsFilesystemRootAndHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		root = filepath.VolumeName(home) + string(filepath.Separator)
	}
	for _, path := range []string{root, home} {
		if err := validatePurgeTarget(path); err == nil {
			t.Errorf("validatePurgeTarget(%q) succeeded, want rejection", path)
		}
	}
}
