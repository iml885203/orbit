package envsync

import (
	"fmt"
	"os"
	"path/filepath"
)

// SyncFromRepo shallow-clones url into a temp directory and copies its
// envs/ subtree into destDir. The temp clone is removed before returning.
func SyncFromRepo(url, destDir string, opts Options) (Result, error) {
	tmp, err := os.MkdirTemp("", "orbit-envsync-")
	if err != nil {
		return Result{}, fmt.Errorf("mktemp: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	cloneDir := filepath.Join(tmp, "repo")
	if err := Clone(url, cloneDir); err != nil {
		return Result{}, err
	}

	envsDir := filepath.Join(cloneDir, "envs")
	info, err := os.Stat(envsDir)
	if err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("repo at %s has no envs/ directory", url)
	}

	return Sync(envsDir, destDir, opts)
}
