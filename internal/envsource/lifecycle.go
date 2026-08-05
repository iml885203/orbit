package envsource

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/iml885203/orbit/atomicio"
	"github.com/iml885203/orbit/internal/envsync"
)

// Refresh owns the sync metadata transition shared by CLI, init, and daemon
// surfaces. When persist is true, failure health and successful provenance are
// written back to an existing registry entry.
func Refresh(registry *Registry, source Source, orbitHome string, dryRun, persist bool) (Source, SyncResult, error) {
	var stored Source
	var hadStored bool
	var versionsBefore map[string]bool
	if persist {
		if existing, getErr := registry.Get(source.Name); getErr == nil {
			stored, hadStored = existing, true
		}
		versionsBefore = sourceVersions(orbitHome, source.Name)
	}
	result, err := Sync(source, orbitHome, dryRun)
	if err != nil {
		if persist && !dryRun {
			health := source
			if hadStored {
				health = stored
			}
			health.LastSyncError = err.Error()
			health.URL = envsync.RedactURL(health.URL)
			if replaceErr := registry.Replace(health); replaceErr != nil {
				return health, result, errors.Join(err, replaceErr)
			}
			source = health
		}
		return source, result, err
	}
	if dryRun {
		return source, result, nil
	}
	source.Commit = result.Commit
	source.ResolvedRef = result.Ref
	source.LastSyncAt = time.Now().UTC()
	source.LastSyncError = ""
	source.URL = envsync.RedactURL(source.URL)
	if persist {
		if err := registry.ReplaceExact(source); err != nil {
			if rollbackErr := rollbackActivatedCache(orbitHome, source.Name, versionsBefore); rollbackErr != nil {
				return source, result, errors.Join(err, rollbackErr)
			}
			return source, result, err
		}
	}
	return source, result, nil
}

// ApplyProposedUpdate commits one complete source edit. Content edits validate
// and activate the proposed source before one registry write; metadata-only
// edits do not access the source.
func ApplyProposedUpdate(registry *Registry, source Source, orbitHome string, contentChanged bool) (Source, SyncResult, error) {
	if contentChanged {
		return Refresh(registry, source, orbitHome, false, true)
	}
	if err := registry.ReplaceExact(source); err != nil {
		return source, SyncResult{}, err
	}
	return source, SyncResult{}, nil
}

func sourceVersions(orbitHome, name string) map[string]bool {
	entries, _ := os.ReadDir(filepath.Join(SourceDir(orbitHome, name), "versions"))
	versions := make(map[string]bool, len(entries))
	for _, entry := range entries {
		versions[entry.Name()] = true
	}
	return versions
}

func rollbackActivatedCache(orbitHome, name string, before map[string]bool) error {
	versionsDir := filepath.Join(SourceDir(orbitHome, name), "versions")
	entries, err := os.ReadDir(versionsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect source versions for rollback: %w", err)
	}
	var archived string
	for _, entry := range entries {
		if !before[entry.Name()] {
			archived = filepath.Join(versionsDir, entry.Name())
		}
	}
	current := CacheDir(orbitHome, name)
	if archived == "" {
		return os.RemoveAll(current)
	}
	failed := filepath.Join(SourceDir(orbitHome, name), ".failed-activation")
	_ = os.RemoveAll(failed)
	if err := os.Rename(current, failed); err != nil {
		return fmt.Errorf("stage failed source cache: %w", err)
	}
	if err := os.Rename(archived, current); err != nil {
		_ = os.Rename(failed, current)
		return fmt.Errorf("restore previous source cache: %w", err)
	}
	return os.RemoveAll(failed)
}

// RemoveOwned stages Orbit-owned cache and selection changes before committing
// the registry removal, and rolls them back if that commit fails. Local source
// directories are never touched.
func RemoveOwned(registry *Registry, orbitHome, name, selectionFile, selection string) (Source, error) {
	_, err := registry.Get(name)
	if err != nil {
		return Source{}, err
	}
	ownedCache := SourceDir(orbitHome, name)
	quarantine := filepath.Join(orbitHome, "sources", "."+name+"-removing")
	cacheStaged := false
	if _, statErr := os.Stat(ownedCache); statErr == nil {
		if err := os.Rename(ownedCache, quarantine); err != nil {
			return Source{}, fmt.Errorf("stage source cache removal: %w", err)
		}
		cacheStaged = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Source{}, fmt.Errorf("inspect source cache: %w", statErr)
	}
	selectionRemoved := ContainsPath(orbitHome, name, selection)
	rollback := func() {
		if selectionRemoved {
			_ = atomicio.WriteFile(selectionFile, []byte(selection+"\n"), 0644)
		}
		if cacheStaged {
			_ = os.Rename(quarantine, ownedCache)
		}
	}
	if selectionRemoved {
		if err := os.Remove(selectionFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollback()
			return Source{}, fmt.Errorf("clear selected environment: %w", err)
		}
	}
	removed, err := registry.Remove(name)
	if err != nil {
		rollback()
		return Source{}, err
	}
	if cacheStaged {
		if err := os.RemoveAll(quarantine); err != nil {
			return removed, fmt.Errorf("remove source cache: %w", err)
		}
	}
	return removed, nil
}
