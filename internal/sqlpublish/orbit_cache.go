package sqlpublish

// The orbit cache base directory — shared by every persistent record
// this package keeps (dacpac build cache, publish-state records,
// diff-result cache).

import (
	"os"
	"path/filepath"
)

// orbitCacheDir returns ~/.orbit/cache/<sub> (honouring ORBIT_HOME),
// creating it. It intentionally re-derives the orbit base dir rather than
// calling daemon.OrbitDir: sqlpublish is a self-contained host-side build
// tool (one internal dependency), and importing daemon would drag its whole
// server dependency graph (6 orbit packages) into it. The rules match
// daemon.OrbitDir; the one deliberate difference is that a home-dir lookup
// failure is propagated here (every caller is best-effort) rather than
// swallowed.
func orbitCacheDir(sub string) (string, error) {
	base := os.Getenv("ORBIT_HOME")
	if base == "" {
		if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
			base = filepath.Join(localApp, "orbit")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".orbit")
		}
	}
	dir := filepath.Join(base, "cache", sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
