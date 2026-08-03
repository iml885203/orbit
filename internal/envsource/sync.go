package envsource

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/envsync"
)

type SyncResult struct {
	Written []string
	Commit  string
	Ref     string
}

func NormalizeExistingDirectory(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("directory path is required")
	}
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if raw == "~" {
			raw = home
		} else {
			raw = filepath.Join(home, raw[2:])
		}
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve directory: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("access directory %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return filepath.Clean(abs), nil
}

func ValidateLocalSource(path string) (string, error) {
	normalized, err := NormalizeExistingDirectory(path)
	if err != nil {
		return "", err
	}
	envs := filepath.Join(normalized, "envs")
	info, err := os.Stat(envs)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("local source %s must contain an envs directory", normalized)
	}
	return normalized, nil
}

func Sync(source Source, orbitHome string, dryRun bool) (SyncResult, error) {
	if err := source.Validate(); err != nil {
		return SyncResult{}, err
	}
	parent := SourceDir(orbitHome, source.Name)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return SyncResult{}, fmt.Errorf("create source cache root: %w", err)
	}
	stage, err := os.MkdirTemp(parent, "."+source.Name+"-sync-")
	if err != nil {
		return SyncResult{}, fmt.Errorf("create source staging cache: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()

	destination := filepath.Join(stage, "envs")
	var synced envsync.Result
	switch source.Type {
	case TypeGit:
		synced, err = envsync.SyncFromRepo(source.URL, source.Ref, destination, envsync.Options{})
	case TypeLocal:
		var normalized string
		normalized, err = ValidateLocalSource(source.Path)
		if err == nil {
			synced, err = envsync.Sync(filepath.Join(normalized, "envs"), destination, envsync.Options{})
		}
	}
	if err != nil {
		return SyncResult{}, err
	}
	if err := validateCache(destination, source.Workspace); err != nil {
		return SyncResult{}, err
	}

	written, err := cacheChanges(EnvsDir(orbitHome, source.Name), destination)
	if err != nil {
		return SyncResult{}, err
	}
	result := SyncResult{Written: written, Commit: synced.Source.Commit, Ref: synced.Source.ResolvedRef}
	if dryRun {
		return result, nil
	}
	if err := replaceCache(CacheDir(orbitHome, source.Name), stage); err != nil {
		return SyncResult{}, err
	}
	return result, nil
}

func cacheChanges(current, proposed string) ([]string, error) {
	currentFiles, err := cacheFileContents(current)
	if err != nil {
		return nil, err
	}
	proposedFiles, err := cacheFileContents(proposed)
	if err != nil {
		return nil, err
	}
	changed := make([]string, 0)
	for path, proposedData := range proposedFiles {
		if currentData, ok := currentFiles[path]; !ok || !bytes.Equal(currentData, proposedData) {
			changed = append(changed, path)
		}
	}
	for path := range currentFiles {
		if _, ok := proposedFiles[path]; !ok {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed, nil
}

func cacheFileContents(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = data
		return nil
	})
	return files, err
}

func validateCache(envsDir, workspace string) error {
	files, err := filepath.Glob(filepath.Join(envsDir, "*.yaml"))
	if err != nil {
		return fmt.Errorf("list source environments: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("source contains no environment YAML files")
	}
	sort.Strings(files)
	previous, hadWorkspace := os.LookupEnv("WORKSPACE_ROOT")
	if workspace != "" {
		_ = os.Setenv("WORKSPACE_ROOT", workspace)
	} else {
		_ = os.Unsetenv("WORKSPACE_ROOT")
	}
	defer func() {
		if hadWorkspace {
			_ = os.Setenv("WORKSPACE_ROOT", previous)
		} else {
			_ = os.Unsetenv("WORKSPACE_ROOT")
		}
	}()
	for _, file := range files {
		if _, err := config.Load(file); err != nil {
			return fmt.Errorf("validate %s: %w", filepath.Base(file), err)
		}
	}
	return nil
}

func replaceCache(target, stage string) error {
	versions := filepath.Join(filepath.Dir(target), "versions")
	if err := os.MkdirAll(versions, 0755); err != nil {
		return fmt.Errorf("create source version archive: %w", err)
	}
	backup := filepath.Join(versions, time.Now().UTC().Format("20060102T150405.000000000Z"))
	hadTarget := false
	if _, err := os.Stat(target); err == nil {
		hadTarget = true
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("preserve previous source cache: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect source cache: %w", err)
	}
	if err := os.Rename(stage, target); err != nil {
		if hadTarget {
			_ = os.Rename(backup, target)
		}
		return fmt.Errorf("activate source cache: %w", err)
	}
	return nil
}
