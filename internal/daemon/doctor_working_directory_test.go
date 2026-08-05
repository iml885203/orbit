package daemon

import (
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestServiceWorkingDirectoryChecksExplainsUnresolvedWorkspace(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "python", Path: filepath.Join(t.TempDir(), "${WORKSPACE_ROOT}", "api")},
	}}

	checks := ServiceWorkingDirectoryChecks(cfg, nil)
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want one failure", checks)
	}
	if checks[0].Name != "Working directory (api)" {
		t.Fatalf("name = %q", checks[0].Name)
	}
	if checks[0].Hint != `run: orbit source update <source> --workspace "$PWD"` {
		t.Fatalf("hint = %q", checks[0].Hint)
	}
}

func TestServiceWorkingDirectoryChecksAcceptsExistingDirectory(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "python", Path: t.TempDir()},
	}}
	if checks := ServiceWorkingDirectoryChecks(cfg, nil); len(checks) != 0 {
		t.Fatalf("checks = %+v, want no failures", checks)
	}
}

func TestServiceWorkingDirectoryChecksHonorsEmptySelection(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "python", Path: filepath.Join(t.TempDir(), "missing")},
	}}
	if checks := ServiceWorkingDirectoryChecks(cfg, []string{}); len(checks) != 0 {
		t.Fatalf("checks = %+v, want unrelated services skipped", checks)
	}
}

func TestServiceWorkingDirectoryChecksDoesNotCallAnotherVariableWorkspaceRoot(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "python", Path: filepath.Join(t.TempDir(), "${API_ROOT}", "api")},
	}}

	checks := ServiceWorkingDirectoryChecks(cfg, nil)
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want one failure", checks)
	}
	if checks[0].Message != "path variable API_ROOT is unresolved in "+cfg.Services["api"].Path {
		t.Fatalf("message = %q", checks[0].Message)
	}
	if checks[0].Hint != `run: orbit settings set-env API_ROOT "$PWD"` {
		t.Fatalf("hint = %q", checks[0].Hint)
	}
}
