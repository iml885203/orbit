// Package envsync copies env configuration files from a source directory
// (typically a freshly-cloned env repo's envs/ subdir) into a destination
// (typically ~/.orbit/envs/). *.yaml files plus everything under seeds/
// (seed scripts, which the envs reference but aren't yaml) are copied;
// subdirectory structure is preserved.
package envsync

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Options tunes Sync behavior.
type Options struct {
	// DryRun reports what would be written without touching the filesystem.
	DryRun bool
}

// Result summarizes a Sync run.
type Result struct {
	// Written lists changed relative paths (relative to destDir) that were
	// written, or would be written in DryRun mode, sorted.
	Written []string
}

// Sync copies env files from srcDir into destDir, preserving subdir
// structure: every *.yaml, plus everything under seeds/ (seed scripts are
// .sql/.js, not yaml, but the envs reference them so they must ride along).
// Existing files at destDir are overwritten.
func Sync(srcDir, destDir string, opts Options) (Result, error) {
	var written []string

	walkErr := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		// Record the slash-normalized form so the Written list (surfaced in
		// `orbit env sync` output) is identical across platforms; the copy
		// below still uses the OS-native rel.
		relSlash := filepath.ToSlash(rel)
		if !strings.HasSuffix(info.Name(), ".yaml") && !strings.HasPrefix(relSlash, "seeds/") {
			return nil
		}
		destination := filepath.Join(destDir, rel)
		unchanged, err := sameFileContents(path, destination)
		if err != nil {
			return err
		}
		if unchanged {
			return nil
		}
		written = append(written, relSlash)
		if opts.DryRun {
			return nil
		}
		return copyFile(path, destination)
	})
	if walkErr != nil {
		return Result{}, walkErr
	}

	sort.Strings(written)
	return Result{Written: written}, nil
}

func sameFileContents(left, right string) (bool, error) {
	leftData, err := os.ReadFile(left)
	if err != nil {
		return false, err
	}
	rightData, err := os.ReadFile(right)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftData, rightData), nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}
