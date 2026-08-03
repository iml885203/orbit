package envsource

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// LoadMigratingLegacy owns the one-time transition from the former global
// environment repository into the source registry. Every presentation surface
// uses this entry point so the dashboard cannot bypass an offline migration.
func LoadMigratingLegacy(orbitHome string, legacy LegacyMigration) (*Registry, error) {
	registry, err := Load(RegistryPath(orbitHome))
	if err != nil || len(registry.List()) > 0 || legacy.URL == "" {
		return registry, err
	}
	source := Source{Name: "default", Type: TypeGit, URL: envsync.RedactURL(legacy.URL), Ref: legacy.Ref, Workspace: legacy.Workspace}
	provenance, _ := envsync.ReadRepositorySource(legacy.EnvsDir)
	if source.Ref == "" {
		source.Ref = provenance.Ref
	}
	source.Commit, source.ResolvedRef = provenance.Commit, provenance.Ref
	files, _ := filepath.Glob(filepath.Join(legacy.EnvsDir, "*.yaml"))
	if len(files) > 0 {
		if _, err := envsync.Sync(legacy.EnvsDir, EnvsDir(orbitHome, source.Name), envsync.Options{}); err != nil {
			return nil, fmt.Errorf("migrate cached environments: %w", err)
		}
	}
	if err := registry.Add(source, true); err != nil {
		_ = os.RemoveAll(SourceDir(orbitHome, source.Name))
		return nil, fmt.Errorf("migrate environment source: %w", err)
	}
	if pathWithinDirectory(legacy.Selection, legacy.EnvsDir) && legacy.SelectionFile != "" {
		migrated := filepath.Join(EnvsDir(orbitHome, source.Name), filepath.Base(legacy.Selection))
		if err := os.WriteFile(legacy.SelectionFile, []byte(migrated+"\n"), 0644); err != nil {
			return nil, fmt.Errorf("migrate environment selection: %w", err)
		}
	}
	if legacy.Clear != nil {
		if err := legacy.Clear(); err != nil {
			return nil, fmt.Errorf("retire legacy environment settings: %w", err)
		}
	}
	return registry, nil
}

func pathWithinDirectory(path, directory string) bool {
	if path == "" {
		return false
	}
	relative, err := filepath.Rel(directory, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) && relative != "."
}
