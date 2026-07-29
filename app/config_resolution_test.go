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
	if err := os.WriteFile(rootConfig, []byte("version: \"2\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := findProjectConfig(nested); got != rootConfig {
		t.Fatalf("findProjectConfig() = %q, want %q", got, rootConfig)
	}

	appConfig := filepath.Join(root, "apps", projectConfigName)
	if err := os.WriteFile(appConfig, []byte("version: \"2\"\n"), 0o600); err != nil {
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
	if err := os.WriteFile(configPath, []byte("version: \"2\"\n"), 0o600); err != nil {
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
	if active.SelectedName != "orbit" || active.SelectedPath != discoveredPath {
		t.Fatalf("active selection = %+v, want project config", active)
	}
	if active.Environments[0].Selected {
		t.Fatal("managed environment must not be marked active while project config wins")
	}
}
