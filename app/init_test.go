package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
)

// setTestExtensions swaps the package extensions for one test.
func setTestExtensions(t *testing.T, exts []extension.Extension) {
	t.Helper()
	prev := extensions
	extensions = exts
	t.Cleanup(func() { extensions = prev })
}

// Two extensions with CLIInit: candidates and markers union in registration
// order, and Steps run for every extension in order.
func TestCLIInitAggregation(t *testing.T) {
	var stepOrder []string
	setTestExtensions(t, []extension.Extension{
		{
			Name: "a",
			CLIInit: &extension.CLIInit{
				WorkspaceCandidates: func(home string) []string { return []string{home + "/a"} },
				WorkspaceMarkers:    func(root string) []string { return []string{"a/"} },
				Steps: func(*daemon.Settings, bool, func(string) string, bool) error {
					stepOrder = append(stepOrder, "a")
					return nil
				},
			},
		},
		{
			Name: "b",
			CLIInit: &extension.CLIInit{
				WorkspaceCandidates: func(home string) []string { return []string{home + "/b"} },
				WorkspaceMarkers:    func(root string) []string { return []string{"b/"} },
				Steps: func(*daemon.Settings, bool, func(string) string, bool) error {
					stepOrder = append(stepOrder, "b")
					return nil
				},
			},
		},
	})

	if got := workspaceCandidates("/h"); len(got) != 2 || got[0] != "/h/a" || got[1] != "/h/b" {
		t.Errorf("candidates = %v, want union in registration order", got)
	}
	if got := workspaceMarkers("/x"); len(got) != 2 || got[0] != "a/" || got[1] != "b/" {
		t.Errorf("markers = %v, want union in registration order", got)
	}
	for _, ext := range extensions {
		if err := ext.CLIInit.Steps(nil, true, nil, false); err != nil {
			t.Fatal(err)
		}
	}
	if len(stepOrder) != 2 || stepOrder[0] != "a" || stepOrder[1] != "b" {
		t.Errorf("step order = %v", stepOrder)
	}
}

func TestConfigureRequiredWorkspaceUsesSavedWorkspaceRoot(t *testing.T) {
	previousYes := initYes
	initYes = true
	t.Cleanup(func() { initYes = previousYes })
	unsetEnvForTest(t, "WORKSPACE_ROOT")

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), "dev.yaml")
	if err := os.WriteFile(envPath, []byte("version: \"3\"\nservices:\n  api:\n    type: python\n    path: ${WORKSPACE_ROOT}/api\n    command: python3 app.py\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := daemon.LoadSettings(filepath.Join(t.TempDir(), "settings.json"))
	if err := settings.Set("workspace_root", root); err != nil {
		t.Fatal(err)
	}

	configured, got, err := configureRequiredWorkspace(initPrinter{}, settings, envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !configured || got != root {
		t.Fatalf("configured = %v, root = %q, want %q", configured, got, root)
	}
	if saved := settings.Get("workspace_root"); saved != root {
		t.Fatalf("saved workspace = %q, want %q", saved, root)
	}
	if applied := os.Getenv("WORKSPACE_ROOT"); applied != root {
		t.Fatalf("WORKSPACE_ROOT = %q, want %q for init health checks", applied, root)
	}
}

func TestConfigureRequiredWorkspaceDoesNotGuessRemoteWorkspaceInYesMode(t *testing.T) {
	previousYes := initYes
	initYes = true
	t.Cleanup(func() { initYes = previousYes })
	unsetEnvForTest(t, "WORKSPACE_ROOT")

	envPath := filepath.Join(t.TempDir(), "dev.yaml")
	if err := os.WriteFile(envPath, []byte("version: \"3\"\nservices:\n  api:\n    type: python\n    path: ${WORKSPACE_ROOT}/api\n    command: python3 app.py\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := daemon.LoadSettings(filepath.Join(t.TempDir(), "settings.json"))

	configured, root, err := configureRequiredWorkspace(initPrinter{}, settings, envPath)
	if err != nil {
		t.Fatal(err)
	}
	if configured || root != "" || settings.Get("workspace_root") != "" {
		t.Fatalf("configured = %v, root = %q, saved = %q", configured, root, settings.Get("workspace_root"))
	}
}

func TestRequiresWorkspaceRootIgnoresCommentsAndFallbacks(t *testing.T) {
	if requiresWorkspaceRoot([]byte("# ${WORKSPACE_ROOT}\nnotes: portable # ${WORKSPACE_ROOT}\npath: ${WORKSPACE_ROOT:-/portable}\n")) {
		t.Fatal("comments or fallback triggered a required workspace")
	}
	if !requiresWorkspaceRoot([]byte("path: ${WORKSPACE_ROOT}/api\n")) {
		t.Fatal("required workspace reference was missed")
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	value, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, value)
			return
		}
		_ = os.Unsetenv(key)
	})
}
