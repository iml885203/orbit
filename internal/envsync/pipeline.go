package envsync

import (
	"fmt"
	"os"
	"path/filepath"
)

// SyncFromRepo resolves ref, shallow-clones that repository state, and copies
// its envs/ subtree into destDir. The temp clone is removed before returning.
func SyncFromRepo(url, ref, destDir string, opts Options) (Result, error) {
	tmp, err := os.MkdirTemp("", "orbit-envsync-")
	if err != nil {
		return Result{}, fmt.Errorf("mktemp: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	cloneDir := filepath.Join(tmp, "repo")
	commit, err := CloneAt(url, ref, cloneDir)
	if err != nil {
		return Result{}, err
	}

	envsDir := filepath.Join(cloneDir, "envs")
	info, err := os.Stat(envsDir)
	if err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("repo at %s has no envs/ directory", url)
	}

	result, err := Sync(envsDir, destDir, opts)
	if err != nil {
		return Result{}, err
	}
	result.Source = RepositorySource{
		URL:    displayURL(url),
		Ref:    ref,
		Commit: commit,
	}
	if !opts.DryRun {
		if err := writeRepositorySource(destDir, result.Source); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}
