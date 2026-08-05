package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/envsource"
)

func TestManagedEnvironmentResolutionUsesQualifiedIdentityOrDefaultOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	registry, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "company", Type: envsource.TypeGit, URL: "https://example.com/company.git"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "env-dev", Type: envsource.TypeLocal, Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"company", "env-dev"} {
		dir := envsource.EnvsDir(home, source)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "e2e.yaml"), []byte("version: \"3\"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	bare, err := resolveEnvArg("e2e")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(envsource.EnvsDir(home, "company"), "e2e.yaml"); bare != want {
		t.Fatalf("bare environment = %q, want first source %q", bare, want)
	}
	qualified, err := resolveEnvArg("env-dev/e2e")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(envsource.EnvsDir(home, "env-dev"), "e2e.yaml"); qualified != want {
		t.Fatalf("qualified environment = %q, want %q", qualified, want)
	}
}

func TestEnvironmentSelectionPreservesQualifiedUnavailableIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	registry, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "company", Type: envsource.TypeGit, URL: "https://example.com/company.git"}); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(envsource.EnvsDir(home, "company"), "removed.yaml")
	if err := os.WriteFile(filepath.Join(home, "current"), []byte(selected+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	selection := readEnvironmentSelection()
	if selection.State != environmentSelectionUnavailable {
		t.Fatalf("state = %q", selection.State)
	}
	if selection.SelectedPath != selected {
		t.Fatalf("selected path = %q", selection.SelectedPath)
	}
	if selection.SelectedIdentity != "company/removed" {
		t.Fatalf("selected identity = %q", selection.SelectedIdentity)
	}
	if got := daemon.OrbitDir(); got != home {
		t.Fatalf("OrbitDir = %q", got)
	}
}

func TestLegacySingleSourceMigratesOfflineWithSelectionAndWorkspace(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	settings := daemon.LoadSettings(daemon.DefaultSettingsPath())
	if err := settings.Set("env_repo_url", "https://example.com/environments.git"); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set("env_repo_ref", "release"); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set("workspace_root", workspace); err != nil {
		t.Fatal(err)
	}
	legacyDir := envsDestDir()
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyEnvironment := filepath.Join(legacyDir, "e2e.yaml")
	if err := os.WriteFile(legacyEnvironment, []byte("version: \"3\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "current"), []byte(legacyEnvironment+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	registry, err := sourceRegistry()
	if err != nil {
		t.Fatal(err)
	}
	source, err := registry.First()
	if err != nil {
		t.Fatal(err)
	}
	if source.Name != "default" || source.Workspace != workspace || source.Ref != "release" {
		t.Fatalf("migrated source = %#v", source)
	}
	migratedEnvironment := filepath.Join(envsource.EnvsDir(home, "default"), "e2e.yaml")
	if selected := readCurrentEnv(); selected != migratedEnvironment {
		t.Fatalf("migrated selection = %q, want %q", selected, migratedEnvironment)
	}
	if _, err := os.Stat(migratedEnvironment); err != nil {
		t.Fatalf("migrated cache: %v", err)
	}
	reloadedSettings := daemon.LoadSettings(daemon.DefaultSettingsPath())
	for _, key := range []string{"env_repo_url", "env_repo_ref", "workspace_root"} {
		if value := reloadedSettings.Get(key); value != "" {
			t.Fatalf("legacy setting %s remains %q", key, value)
		}
	}
}
