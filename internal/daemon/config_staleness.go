// Config staleness detection: the daemon runs on the config it last applied,
// while the user keeps editing env
// files and switching selections. This file answers "does what I loaded
// still match reality?" so status can offer a safe environment apply
// instead of silently serving stale state.

package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"time"

	"github.com/iml885203/orbit/config"
)

// configBaseline captures what the daemon loaded, for later comparison.
// Guarded by Server.pathMu (the same mutex that guards configPath —
// path and baseline always change together).
type configBaseline struct {
	// fromCurrent records whether the loaded path came from the
	// ~/.orbit/current selection. A daemon started with an explicit -c
	// flag intentionally diverges from current, so selection-changed
	// staleness never fires for it (it would be a permanent false alarm).
	fromCurrent bool
	files       map[string]configFileBaseline
}

type configFileBaseline struct {
	hash  string
	mtime time.Time
	size  int64
}

// fileStamp hashes a config file. ok=false when the file can't be read —
// callers treat that as "unknown", never as stale (transient editor saves
// and atomic-rename windows shouldn't flap the flag).
func fileStamp(path string) (hash string, mtime time.Time, size int64, ok bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", time.Time{}, 0, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", time.Time{}, 0, false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), info.ModTime(), info.Size(), true
}

// recordConfigBaselineLocked snapshots the just-loaded config file. Caller
// must hold s.pathMu (write) — SetConfigPath is the single choke point.
func (s *Server) recordConfigBaselineLocked(path string) {
	paths, err := config.InheritanceFiles(path)
	if err != nil {
		s.baseline = configBaseline{}
		return
	}
	files := make(map[string]configFileBaseline, len(paths))
	for _, referencedPath := range paths {
		hash, mtime, size, ok := fileStamp(referencedPath)
		if !ok {
			s.baseline = configBaseline{}
			return
		}
		files[referencedPath] = configFileBaseline{hash: hash, mtime: mtime, size: size}
	}
	s.baseline = configBaseline{fromCurrent: path == ReadCurrentEnv(), files: files}
}

// fileEdited reports whether the config file's bytes changed since the
// baseline. One stat owns both the mtime+size fast path and the rehash
// decision — status polls every couple of seconds, and hashing yaml on
// each poll would be wasted work. A touch that doesn't change bytes
// refreshes the cached stamp so the fast path recovers instead of
// rehashing forever.
func (s *Server) fileEdited(configPath, path string, base configFileBaseline) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.ModTime().Equal(base.mtime) && info.Size() == base.size {
		return false
	}
	hash, mtime, size, ok := fileStamp(path)
	if !ok {
		return false
	}
	if hash == base.hash {
		s.pathMu.Lock()
		// Re-check the path under the lock: an env switch may have moved
		// the baseline to a different file while we hashed this one.
		if s.configPath == configPath {
			files := make(map[string]configFileBaseline, len(s.baseline.files))
			for referencedPath, referencedBaseline := range s.baseline.files {
				files[referencedPath] = referencedBaseline
			}
			current := files[path]
			current.mtime, current.size = mtime, size
			files[path] = current
			s.baseline.files = files
		}
		s.pathMu.Unlock()
		return false
	}
	return true
}

// configStale reports whether the daemon's loaded config has fallen behind
// reality, and why — in the spec's order: selection changed, file edited,
// then the sticky engine flag. Known limits (per the config-holder spec):
// process-env substitution inputs and shared extension data files such as
// data/claim.yaml are not covered.
func (s *Server) configStale() (bool, string) {
	s.pathMu.RLock()
	path := s.configPath
	base := s.baseline
	s.pathMu.RUnlock()
	if path == "" || len(base.files) == 0 {
		if s.engineStale.Load() {
			return true, "environment graph needs refresh"
		}
		return false, ""
	}

	if base.fromCurrent {
		if cur := ReadCurrentEnv(); cur != "" && cur != path {
			return true, "env selection changed"
		}
	}
	for referencedPath, fileBaseline := range base.files {
		if s.fileEdited(path, referencedPath, fileBaseline) {
			return true, "env file edited"
		}
	}
	if s.engineStale.Load() {
		return true, "environment graph needs refresh"
	}
	return false, ""
}
