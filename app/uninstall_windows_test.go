//go:build windows

package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsUninstallHelperReadsEveryManifestPath(t *testing.T) {
	dir := t.TempDir()
	targets := []string{
		filepath.Join(dir, "orbit.exe"),
		filepath.Join(dir, "data with spaces"),
	}
	if err := os.WriteFile(targets[0], []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(targets[1], 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targets[1], "settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(dir, "uninstall.json")
	manifest, err := json.Marshal(targets)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	helperPath := filepath.Join(dir, "uninstall.ps1")
	if err := os.WriteFile(helperPath, []byte(windowsUninstallHelper), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", helperPath,
		"-OrbitParentPID", "2147483647",
		"-OrbitManifest", manifestPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		failures, readErr := os.ReadFile(helperPath + ".failed")
		t.Fatalf("run uninstall helper: %v\noutput: %s\nfailures: %s\nread failures: %v", err, output, failures, readErr)
	}

	for _, path := range append(targets, manifestPath, helperPath) {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists", path)
		}
	}
}
