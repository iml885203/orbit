package app

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
)

func TestListEnvFiles_TopLevelOnly(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "development.yaml"), []byte{}, 0644)
	_ = os.WriteFile(filepath.Join(dir, "example.yaml"), []byte{}, 0644)
	_ = os.MkdirAll(filepath.Join(dir, "data"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "data", "kafka-topics.yaml"), []byte{}, 0644)
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte{}, 0644)

	got := listEnvFiles(dir)
	sort.Strings(got)
	want := []string{"development.yaml", "example.yaml"}
	if len(got) != len(want) {
		t.Fatalf("listEnvFiles = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPickDefault_PrefersDistributionDefault(t *testing.T) {
	setTestDistribution(t, extension.Distribution{DefaultEnv: "development.yaml"})
	got := pickDefault([]string{"example.yaml", "development.yaml", "test-minimal.yaml"})
	if got != "development.yaml" {
		t.Errorf("pickDefault = %q, want development.yaml", got)
	}
}

func TestPickDefault_FirstWhenNoDefault(t *testing.T) {
	got := pickDefault([]string{"custom.yaml", "other.yaml"})
	if got != "custom.yaml" {
		t.Errorf("pickDefault = %q, want first", got)
	}
}

func TestResolveInitEnvName(t *testing.T) {
	available := []string{"development.yaml", "example.yaml"}
	tests := []struct {
		in, want string
	}{
		{"development", "development.yaml"},
		{"development.yaml", "development.yaml"},
		{"example", "example.yaml"},
		{"missing", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := resolveInitEnvName(tt.in, available); got != tt.want {
			t.Errorf("resolveInitEnvName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDetectWorkspaceRoot_PrefersCurrentDirectoryWithEnvs(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()

	current := t.TempDir()
	if err := os.Mkdir(filepath.Join(current, "envs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(current); err != nil {
		t.Fatal(err)
	}
	current, err = os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	saved := t.TempDir()
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	settings := daemon.LoadSettings(settingsPath)
	if err := settings.Set("workspace_root", saved); err != nil {
		t.Fatal(err)
	}

	if got := detectWorkspaceRoot(settings); got != current {
		t.Errorf("detectWorkspaceRoot = %q, want current directory %q", got, current)
	}
}

func TestDetectWorkspaceRootDoesNotInventWorkspaceFromCurrentDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()

	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	settings := daemon.LoadSettings(filepath.Join(t.TempDir(), "settings.json"))
	if got := detectWorkspaceRoot(settings); got != "" {
		t.Errorf("detectWorkspaceRoot = %q, want no unproven workspace", got)
	}
}

func TestDetectWorkspaceRoot_PrefersSavedRootOverUnmarkedCurrentDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()

	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	saved := t.TempDir()
	settings := daemon.LoadSettings(filepath.Join(t.TempDir(), "settings.json"))
	if err := settings.Set("workspace_root", saved); err != nil {
		t.Fatal(err)
	}

	if got := detectWorkspaceRoot(settings); got != saved {
		t.Errorf("detectWorkspaceRoot = %q, want saved root %q", got, saved)
	}
}

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

func TestConfigureRequiredWorkspaceUsesLocalEnvironmentRoot(t *testing.T) {
	previousYes := initYes
	initYes = true
	t.Cleanup(func() { initYes = previousYes })
	unsetEnvForTest(t, "WORKSPACE_ROOT")

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), "dev.yaml")
	if err := os.WriteFile(envPath, []byte("version: \"2\"\nservices:\n  api:\n    type: python\n    path: ${WORKSPACE_ROOT}/api\n    command: python3 app.py\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := daemon.LoadSettings(filepath.Join(t.TempDir(), "settings.json"))

	configured, got, err := configureRequiredWorkspace(initPrinter{}, settings, envPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if !configured || got != root {
		t.Fatalf("configured = %v, root = %q, want %q", configured, got, root)
	}
	if saved := settings.Get("workspace_root"); saved != root {
		t.Fatalf("saved workspace = %q, want %q", saved, root)
	}
}

func TestConfigureRequiredWorkspaceDoesNotGuessRemoteWorkspaceInYesMode(t *testing.T) {
	previousYes := initYes
	initYes = true
	t.Cleanup(func() { initYes = previousYes })
	unsetEnvForTest(t, "WORKSPACE_ROOT")

	envPath := filepath.Join(t.TempDir(), "dev.yaml")
	if err := os.WriteFile(envPath, []byte("version: \"2\"\nservices:\n  api:\n    type: python\n    path: ${WORKSPACE_ROOT}/api\n    command: python3 app.py\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := daemon.LoadSettings(filepath.Join(t.TempDir(), "settings.json"))

	configured, root, err := configureRequiredWorkspace(initPrinter{}, settings, envPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if configured || root != "" || settings.Get("workspace_root") != "" {
		t.Fatalf("configured = %v, root = %q, saved = %q", configured, root, settings.Get("workspace_root"))
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
