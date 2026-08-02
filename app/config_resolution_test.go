package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProjectConfigUsesNearestAncestor(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	rootConfig := filepath.Join(root, projectConfigName)
	if err := os.WriteFile(rootConfig, []byte("version: \"3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := findProjectConfig(nested); got != rootConfig {
		t.Fatalf("findProjectConfig() = %q, want %q", got, rootConfig)
	}

	appConfig := filepath.Join(root, "apps", projectConfigName)
	if err := os.WriteFile(appConfig, []byte("version: \"3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := findProjectConfig(nested); got != appConfig {
		t.Fatalf("findProjectConfig() = %q, want nearest %q", got, appConfig)
	}
}

func TestFindProjectConfigReturnsEmptyWithoutOrbitYAML(t *testing.T) {
	if got := findProjectConfig(t.TempDir()); got != "" {
		t.Fatalf("findProjectConfig() = %q, want empty", got)
	}
}

func TestActiveEnvironmentSelectionReportsProjectOverride(t *testing.T) {
	project := t.TempDir()
	configPath := filepath.Join(project, projectConfigName)
	if err := os.WriteFile(configPath, []byte("version: \"3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	discoveredPath := discoverProjectConfig()
	if discoveredPath == "" {
		t.Fatal("project config was not discovered")
	}

	active := activeEnvironmentSelection(environmentSelection{
		State:        environmentSelectionSelected,
		SelectedName: "team",
		SelectedPath: "/managed/team.yaml",
		Environments: []environmentChoice{{
			Name:     "team",
			Path:     "/managed/team.yaml",
			Selected: true,
		}},
	}, discoveredPath)

	if active.Source != "project" {
		t.Fatalf("source = %q, want project", active.Source)
	}
	if active.SelectedName != filepath.Base(project) || active.SelectedPath != discoveredPath {
		t.Fatalf("active selection = %+v, want project config", active)
	}
	if active.Environments[0].Selected {
		t.Fatal("managed environment must not be marked active while project config wins")
	}
	if active.ManagedSelection == nil || active.ManagedSelection.Name != "team" || active.ManagedSelection.Selected {
		t.Fatalf("managed selection = %+v, want preserved but inactive", active.ManagedSelection)
	}
}

func TestExplicitOrbitYAMLIsNotProjectEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), projectConfigName)
	active := activeEnvironmentSelectionWithKind(environmentSelection{
		State:        environmentSelectionSelected,
		SelectedName: "managed",
		SelectedPath: "/envs/managed.yaml",
	}, path, "explicit")

	if active.Source != "explicit" || active.SelectedPath != path {
		t.Fatalf("explicit config was relabeled as project: %+v", active)
	}
	if active.ManagedSelection == nil || active.ManagedSelection.Path != "/envs/managed.yaml" {
		t.Fatalf("managed selection was not preserved: %+v", active.ManagedSelection)
	}
}

func TestSameFilePathResolvesSymlinks(t *testing.T) {
	root := t.TempDir()
	realPath := filepath.Join(root, "real.yaml")
	if err := os.WriteFile(realPath, []byte("version: \"3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(root, "linked.yaml")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !sameFilePath(realPath, linkPath) {
		t.Fatalf("%q and %q should be the same canonical file", realPath, linkPath)
	}
}
