package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestPurgePreviewSeparatesRemovedAndPreservedResources(t *testing.T) {
	var output bytes.Buffer
	printUninstallPlan(&output, uninstallData{
		Artifacts:          []string{"/bin/orbit", "/data/orbit"},
		UserData:           "/data/orbit",
		UserDataPreserved:  false,
		DockerPreserved:    true,
		WorkspacePreserved: true,
	})

	got := output.String()
	removeSection, preserveSection, found := strings.Cut(got, "This will preserve:\n")
	if !found {
		t.Fatalf("preview has no preserve section:\n%s", got)
	}
	if !strings.Contains(removeSection, "user data: /data/orbit") {
		t.Fatalf("remove section does not name user data:\n%s", removeSection)
	}
	if strings.Contains(removeSection, "Docker images") || strings.Contains(removeSection, "project workspaces") {
		t.Fatalf("remove section claims preserved resources:\n%s", removeSection)
	}
	if !strings.Contains(preserveSection, "Docker images and project workspaces") {
		t.Fatalf("preserve section does not name retained resources:\n%s", preserveSection)
	}
}

func TestPurgeResultConfirmsUserDataRemoval(t *testing.T) {
	var output bytes.Buffer
	printUninstallResult(&output, uninstallData{UserData: "/data/orbit"})
	if !strings.Contains(output.String(), "User data removed from /data/orbit.") {
		t.Fatalf("result does not confirm user data removal:\n%s", output.String())
	}
}
