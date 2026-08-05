package envsource

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestLegacyMigrationIsSingleAcrossConcurrentEntryPoints(t *testing.T) {
	orbitHome := t.TempDir()
	legacyEnvs := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacyEnvs, "dev.yaml"), []byte("version: \"3\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var clears atomic.Int32
	legacy := LegacyMigration{URL: "https://example.com/envs.git", EnvsDir: legacyEnvs, Clear: func() error { clears.Add(1); return nil }}
	results := make(chan *LegacyMigrationResult, 2)
	errors := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, result, err := LoadMigratingLegacyWithResult(orbitHome, legacy)
			results <- result
			errors <- err
		}()
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	migrations := 0
	for result := range results {
		if result != nil {
			migrations++
		}
	}
	if migrations != 1 || clears.Load() != 1 {
		t.Fatalf("migrations=%d clears=%d", migrations, clears.Load())
	}
}

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

func TestLegacyMigrationReportsPreservedStateOnce(t *testing.T) {
	orbitHome := t.TempDir()
	legacyEnvs := t.TempDir()
	selectionFile := filepath.Join(orbitHome, "current")
	legacySelection := filepath.Join(legacyEnvs, "development.yaml")
	if err := os.WriteFile(legacySelection, []byte("version: \"3\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selectionFile, []byte(legacySelection+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	registry, result, err := LoadMigratingLegacyWithResult(orbitHome, LegacyMigration{
		URL: "https://example.com/environments.git", Workspace: t.TempDir(), EnvsDir: legacyEnvs,
		Selection: legacySelection, SelectionFile: selectionFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.SourceName != "default" || result.CachedEnvironments != 1 ||
		result.Location != "https://example.com/environments.git" ||
		!result.SelectionPreserved || !result.WorkspacePreserved || !result.Offline {
		t.Fatalf("migration result = %#v", result)
	}
	if len(registry.List()) != 1 {
		t.Fatalf("sources = %#v", registry.List())
	}
	pending, err := ReadLegacyMigrationNotice(orbitHome)
	if err != nil || pending == nil || pending.Location != result.Location {
		t.Fatalf("pending notice = %#v, %v", pending, err)
	}

	_, repeated, err := LoadMigratingLegacyWithResult(orbitHome, LegacyMigration{URL: "https://example.com/environments.git"})
	if err != nil {
		t.Fatal(err)
	}
	if repeated != nil {
		t.Fatalf("repeated migration result = %#v, want nil", repeated)
	}
}
