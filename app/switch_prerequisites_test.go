package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/daemon"
)

func TestSwitchPrerequisitesLeadWithMissingRuntimeBeforePackages(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), "node.yaml")
	raw := fmt.Sprintf(`
version: "3"
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
	var failed []string
	for _, check := range checks {
		if check.Name == "Packages (web)" {
			t.Fatalf("packages were checked before their manager is available: %+v", checks)
		}
		if check.Status == daemon.CheckFail {
			failed = append(failed, check.Name)
		}
	}
	if got := fmt.Sprint(failed); got != "[Node.js pnpm]" {
		t.Fatalf("failed checks = %s, want [Node.js pnpm]", got)
	}
}
