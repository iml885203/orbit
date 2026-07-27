//go:build !windows

package app

import (
	"fmt"
	"os"
)

func removeUninstallArtifacts(paths []string) (bool, error) {
	for i := len(paths) - 1; i >= 0; i-- {
		path := paths[i]
		if err := os.RemoveAll(path); err != nil {
			return false, fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return false, nil
}
