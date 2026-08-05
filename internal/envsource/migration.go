package envsource

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"github.com/iml885203/orbit/atomicio"
	"github.com/iml885203/orbit/internal/envsync"
)

type LegacyMigration struct {
	URL           string
	Ref           string
	Workspace     string
	EnvsDir       string
	Selection     string
	SelectionFile string
	Clear         func() error
}

type LegacyMigrationResult struct {
	SourceName         string `json:"source_name"`
	Location           string `json:"location"`
	Ref                string `json:"ref,omitempty"`
	CachedEnvironments int    `json:"cached_environments"`
	SelectionPreserved bool   `json:"selection_preserved"`
	WorkspacePreserved bool   `json:"workspace_preserved"`
	Offline            bool   `json:"offline"`
}

func migrationNoticePath(orbitHome string) string {
	return filepath.Join(orbitHome, "source-migration-notice.json")
}

func ReadLegacyMigrationNotice(orbitHome string) (*LegacyMigrationResult, error) {
	data, err := os.ReadFile(migrationNoticePath(orbitHome))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read source migration notice: %w", err)
	}
	var result LegacyMigrationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse source migration notice: %w", err)
	}
	return &result, nil
}

func AcknowledgeLegacyMigrationNotice(orbitHome string) error {
	err := os.Remove(migrationNoticePath(orbitHome))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// LoadMigratingLegacy owns the one-time transition from the former global
// environment repository into the source registry. Every presentation surface
// uses this entry point so the dashboard cannot bypass an offline migration.
func LoadMigratingLegacy(orbitHome string, legacy LegacyMigration) (*Registry, error) {
	registry, _, err := LoadMigratingLegacyWithResult(orbitHome, legacy)
	return registry, err
}

func LoadMigratingLegacyWithResult(orbitHome string, legacy LegacyMigration) (*Registry, *LegacyMigrationResult, error) {
	if err := os.MkdirAll(orbitHome, 0755); err != nil {
		return nil, nil, fmt.Errorf("create Orbit home: %w", err)
	}
	migrationLock := flock.New(filepath.Join(orbitHome, ".source-migration.lock"))
	if err := migrationLock.Lock(); err != nil {
		return nil, nil, fmt.Errorf("lock environment source migration: %w", err)
	}
	defer func() { _ = migrationLock.Unlock() }()
	registry, err := Load(RegistryPath(orbitHome))
	if err != nil || len(registry.List()) > 0 || legacy.URL == "" {
		return registry, nil, err
	}
	source := Source{Name: "default", Type: TypeGit, URL: envsync.RedactURL(legacy.URL), Ref: legacy.Ref, Workspace: legacy.Workspace}
	provenance, _ := envsync.ReadRepositorySource(legacy.EnvsDir)
	if source.Ref == "" {
		source.Ref = provenance.Ref
	}
	source.Commit, source.ResolvedRef = provenance.Commit, provenance.Ref
	var previousSelection []byte
	hadSelection := false
	if legacy.SelectionFile != "" {
		previousSelection, err = os.ReadFile(legacy.SelectionFile)
		switch {
		case err == nil:
			hadSelection = true
		case errors.Is(err, os.ErrNotExist):
		default:
			return nil, nil, fmt.Errorf("read legacy environment selection: %w", err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(legacy.EnvsDir, "*.yaml"))
	if len(files) > 0 {
		if _, err := envsync.Sync(legacy.EnvsDir, EnvsDir(orbitHome, source.Name), envsync.Options{}); err != nil {
			return nil, nil, fmt.Errorf("migrate cached environments: %w", err)
		}
	}
	if err := registry.Add(source); err != nil {
		_ = os.RemoveAll(SourceDir(orbitHome, source.Name))
		return nil, nil, fmt.Errorf("migrate environment source: %w", err)
	}
	rollback := func() error {
		var rollbackErrors []error
		if err := AcknowledgeLegacyMigrationNotice(orbitHome); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove source migration notice: %w", err))
		}
		if hadSelection {
			if err := atomicio.WriteFile(legacy.SelectionFile, previousSelection, 0644); err != nil {
				return fmt.Errorf("restore legacy environment selection: %w", err)
			}
		} else if legacy.SelectionFile != "" {
			if err := os.Remove(legacy.SelectionFile); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove migrated environment selection: %w", err)
			}
		}
		if _, err := registry.Remove(source.Name); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove migrated source registry entry: %w", err))
		}
		if err := os.RemoveAll(SourceDir(orbitHome, source.Name)); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove migrated source cache: %w", err))
		}
		return errors.Join(rollbackErrors...)
	}
	if pathWithinDirectory(legacy.Selection, legacy.EnvsDir) && legacy.SelectionFile != "" {
		migrated := filepath.Join(EnvsDir(orbitHome, source.Name), filepath.Base(legacy.Selection))
		if err := atomicio.WriteFile(legacy.SelectionFile, []byte(migrated+"\n"), 0644); err != nil {
			return nil, nil, errors.Join(fmt.Errorf("migrate environment selection: %w", err), rollback())
		}
	}
	result := &LegacyMigrationResult{
		SourceName:         source.Name,
		Location:           source.Location(),
		Ref:                source.Ref,
		CachedEnvironments: len(files),
		SelectionPreserved: pathWithinDirectory(legacy.Selection, legacy.EnvsDir) && legacy.SelectionFile != "",
		WorkspacePreserved: legacy.Workspace != "",
		Offline:            true,
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, nil, errors.Join(err, rollback())
	}
	if err := atomicio.WriteFile(migrationNoticePath(orbitHome), append(data, '\n'), 0644); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("write source migration notice: %w", err), rollback())
	}
	if legacy.Clear != nil {
		if err := legacy.Clear(); err != nil {
			return nil, nil, errors.Join(fmt.Errorf("retire legacy environment settings: %w", err), rollback())
		}
	}
	return registry, result, nil
}

func pathWithinDirectory(path, directory string) bool {
	if path == "" {
		return false
	}
	relative, err := filepath.Rel(directory, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) && relative != "."
}
