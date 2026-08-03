package envsource

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyMigrationRollsBackWhenClearingSettingsFails(t *testing.T) {
	orbitHome := t.TempDir()
	legacyEnvs := filepath.Join(t.TempDir(), "envs")
	if err := os.MkdirAll(legacyEnvs, 0755); err != nil {
		t.Fatal(err)
	}
	legacyConfig := filepath.Join(legacyEnvs, "dev.yaml")
	if err := os.WriteFile(legacyConfig, []byte("version: \"3\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	selectionFile := filepath.Join(orbitHome, "current")
	if err := os.WriteFile(selectionFile, []byte(legacyConfig+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadMigratingLegacy(orbitHome, LegacyMigration{
		URL: "https://example.com/envs.git", EnvsDir: legacyEnvs,
		Selection: legacyConfig, SelectionFile: selectionFile,
		Clear: func() error { return errors.New("settings unavailable") },
	})
	if err == nil {
		t.Fatal("migration succeeded despite settings failure")
	}
	registry, loadErr := Load(RegistryPath(orbitHome))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(registry.List()) != 0 {
		t.Fatalf("half-migrated registry remains: %+v", registry.List())
	}
	selected, readErr := os.ReadFile(selectionFile)
	if readErr != nil || string(selected) != legacyConfig+"\n" {
		t.Fatalf("legacy selection not restored: %q, %v", selected, readErr)
	}
	if _, statErr := os.Stat(SourceDir(orbitHome, "default")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("migrated cache remains: %v", statErr)
	}
}

func TestLegacyMigrationStopsBeforeMutationWhenSelectionCannotBeRead(t *testing.T) {
	orbitHome := t.TempDir()
	legacyEnvs := filepath.Join(t.TempDir(), "envs")
	if err := os.MkdirAll(legacyEnvs, 0755); err != nil {
		t.Fatal(err)
	}
	selectionFile := filepath.Join(orbitHome, "unreadable-selection")
	if err := os.Mkdir(selectionFile, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := LoadMigratingLegacy(orbitHome, LegacyMigration{
		URL: "https://example.com/envs.git", EnvsDir: legacyEnvs, SelectionFile: selectionFile,
	})
	if err == nil {
		t.Fatal("migration succeeded with unreadable selection")
	}
	registry, loadErr := Load(RegistryPath(orbitHome))
	if loadErr != nil || len(registry.List()) != 0 {
		t.Fatalf("migration mutated registry: %+v, %v", registry.List(), loadErr)
	}
	if _, statErr := os.Stat(SourceDir(orbitHome, "default")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("migration mutated cache: %v", statErr)
	}
}
