package sqlpublish

// dacpac build cache. A `dotnet build` of a SQL project is the slow part
// of every publish/diff (cold ~30s, warm ~10s), while the actual
// sqlpackage step is ~6s. When the project's source hasn't changed since
// the last build, the produced dacpac is byte-for-byte the same — so we
// key a cache on a fingerprint of the project's source files (see
// source_fingerprint.go) and reuse the stored dacpac, skipping dotnet
// entirely.
//
// This matters most for diff/check-all: scanning N databases would
// otherwise rebuild every project even though nothing changed since the
// developer last published.
//
// The cache is best-effort: any error (unreadable dir, unwritable cache)
// falls through to a normal build. A stale or wrong cache entry is
// impossible to serve because the fingerprint changes whenever any source
// file's path/size/mtime changes; the sqlpackage version is folded in so a
// toolchain upgrade also invalidates.

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

const cacheCompleteFile = ".complete"

// cachedBuildDir returns the per-fingerprint cache directory holding a
// build's dacpac output, or ("", err) when the cache root is unavailable.
// The whole build output is cached, not just the leaf dacpac: a project
// with <ProjectReference>s emits a referenced dacpac (e.g. CommonFiles.dacpac)
// alongside its own, and sqlpackage DeployReport/Publish needs all of them
// in one directory to resolve external references.
func cachedBuildDir(fingerprint string) (string, error) {
	dir, err := orbitCacheDir("dacpac")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fingerprint), nil
}

// restoreDacpacs copies every *.dacpac from a cached build dir into dst.
// Returns the count restored; a zero count (or any copy error) means the
// caller should fall through to a real build.
func restoreDacpacs(cacheDir, dst string) (int, error) {
	if _, err := os.Stat(filepath.Join(cacheDir, cacheCompleteFile)); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".dacpac") {
			continue
		}
		if err := copyFile(filepath.Join(cacheDir, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// storeDacpacs copies every *.dacpac produced in srcDir into the cache dir
// (created fresh). Best-effort — the caller ignores errors.
func storeDacpacs(srcDir, cacheDir string) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(cacheDir, cacheCompleteFile))
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".dacpac") {
			continue
		}
		if err := copyFileAtomic(filepath.Join(srcDir, e.Name()), filepath.Join(cacheDir, e.Name())); err != nil {
			return err
		}
	}
	complete := filepath.Join(cacheDir, cacheCompleteFile)
	return os.WriteFile(complete, nil, 0o644)
}

func copyFileAtomic(src, dst string) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".dacpac-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	in, err := os.Open(src)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	defer func() { _ = in.Close() }()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, dst)
}

// copyFile copies src to dst, creating/truncating dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
