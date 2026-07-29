package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/daemon"
)

func TestSwitchPrerequisitesUseSelectedEnvironmentPackages(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), "node.yaml")
	raw := fmt.Sprintf(`
version: "2"
services:
  web:
    type: node
    path: %q
    command: pnpm dev
`, project)
	if err := os.WriteFile(envPath, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")

	checks, ready, err := switchPrerequisites(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("prerequisites ready despite missing runtime and packages")
	}
	for _, check := range checks {
		if check.Name == "Packages (web)" && check.Status == daemon.CheckFail {
			return
		}
	}
	t.Fatalf("checks = %+v", checks)
}
