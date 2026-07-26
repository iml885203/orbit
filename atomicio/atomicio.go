// Package atomicio provides crash-safe file writes via temp-file + rename.
// Used wherever we maintain a small JSON state file the daemon and CLI
// must not see in a torn state.
package atomicio

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile writes data to path via a temp file in the same directory,
// then renames. Rename is atomic on POSIX when source and dest are on the
// same filesystem, which is guaranteed because the temp file is created
// in path's directory.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("renaming to %s: %w", path, err)
	}
	return nil
}
