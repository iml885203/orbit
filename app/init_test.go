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

func TestDetectWorkspaceRoot_FallsBackToCurrentDirectory(t *testing.T) {
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
	if err := os.Chdir(current); err != nil {
		t.Fatal(err)
	}
	current, err = os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	settings := daemon.LoadSettings(filepath.Join(t.TempDir(), "settings.json"))
	if got := detectWorkspaceRoot(settings); got != current {
		t.Errorf("detectWorkspaceRoot = %q, want current directory %q", got, current)
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

// Two extensions with CLIInit: candidates and markers union in
// registration order; the hint is first-non-empty-wins; Steps run for
// every extension in order.
func TestCLIInitAggregation(t *testing.T) {
	var stepOrder []string
	setTestExtensions(t, []extension.Extension{
		{
			Name: "a",
			CLIInit: &extension.CLIInit{
				WorkspaceCandidates: func(home string) []string { return []string{home + "/a"} },
				WorkspaceMarkers:    func(root string) []string { return []string{"a/"} },
				MarkerHint:          "a/ expected",
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
				MarkerHint:          "b/ expected",
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
	if got := workspaceMarkerHint(); got != "a/ expected" {
		t.Errorf("hint = %q, want first extension's", got)
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

// workspaceExample contracts the home prefix to ~ and leaves
// non-home-based candidates untouched.
func TestWorkspaceExample(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	setTestExtensions(t, []extension.Extension{{
		Name: "x",
		CLIInit: &extension.CLIInit{
			WorkspaceCandidates: func(h string) []string { return []string{filepath.Join(h, "dev", "example")} },
		},
	}})
	want := "~" + string(filepath.Separator) + filepath.Join("dev", "example")
	if got := workspaceExample(); got != want {
		t.Errorf("workspaceExample = %q, want %q", got, want)
	}

	setTestExtensions(t, []extension.Extension{{
		Name: "x",
		CLIInit: &extension.CLIInit{
			WorkspaceCandidates: func(string) []string { return []string{"/opt/work"} },
		},
	}})
	if got := workspaceExample(); got != "/opt/work" {
		t.Errorf("workspaceExample = %q, want /opt/work untouched", got)
	}

	setTestExtensions(t, nil)
	if got := workspaceExample(); got != "~/dev/workspace" {
		t.Errorf("workspaceExample = %q, want generic placeholder", got)
	}
	_ = home
}
