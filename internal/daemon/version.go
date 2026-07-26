package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// probeVersion execs `<path> --version` with a short timeout and returns the
// trimmed stdout. Overridable in tests.
var probeVersion = func(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// resolveSymlinks canonicalises a path via EvalSymlinks, falling back to the
// raw path if evaluation fails (e.g. broken symlink, missing file).
func resolveSymlinks(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// candidatePathsFn produces the list of on-disk orbit binaries to probe.
// Overridable in tests to inject a fixed list.
var candidatePathsFn = candidatePaths

// candidatePaths collects possible on-disk orbit binaries in priority order,
// returning only existing, deduplicated (by resolved symlink target) paths.
func candidatePaths() []string {
	raw := make([]string, 0, 4)
	if exe, err := os.Executable(); err == nil {
		raw = append(raw, exe)
	}
	if p, err := exec.LookPath("orbit"); err == nil {
		raw = append(raw, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		raw = append(raw, filepath.Join(home, ".local", "bin", "orbit"))
	}
	raw = append(raw, "/usr/local/bin/orbit")
	return filterExistingAndDedup(raw)
}

// filterExistingAndDedup drops non-existent paths and collapses duplicates
// that share a resolved symlink target, preserving input order.
func filterExistingAndDedup(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		resolved := resolveSymlinks(p)
		if _, dup := seen[resolved]; dup {
			continue
		}
		seen[resolved] = struct{}{}
		out = append(out, p)
	}
	return out
}

// parseVersionToken extracts the leading whitespace-separated token from a
// `--version` output line — e.g. "c9c714b-dirty (…)" → "c9c714b-dirty".
func parseVersionToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

// sameVersion returns true if two version tokens represent the same build.
// Both dirty with the same pre-dirty hash are treated as identical; a dirty
// vs clean pair is NOT (the clean build is meaningfully different).
func sameVersion(a, b string) bool {
	if a == b {
		return true
	}
	aDirty := strings.HasSuffix(a, "-dirty")
	bDirty := strings.HasSuffix(b, "-dirty")
	if aDirty && bDirty {
		return strings.TrimSuffix(a, "-dirty") == strings.TrimSuffix(b, "-dirty")
	}
	return false
}

// detectUpdate scans candidate orbit binaries for one whose version differs
// from the daemon's running version. Returns the candidate's full version
// string and its (un-resolved) path, or empty strings when no update found.
// The daemon's self-exe path is NOT skipped — a user may overwrite that very
// file (install.sh does exactly this), and the on-disk binary's version can
// then meaningfully differ from s.version (the daemon's in-memory build).
func detectUpdate(running string) (onDisk, onDiskPath string) {
	runningToken := parseVersionToken(running)

	for _, path := range candidatePathsFn() {
		out, err := probeVersion(path)
		if err != nil {
			slog.Debug("version probe failed", "component", "version", "path", path, "err", err)
			continue
		}
		token := parseVersionToken(out)
		if token == "" || sameVersion(token, runningToken) {
			continue
		}
		return out, path
	}
	return "", ""
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Error: "method not allowed"})
		return
	}
	resp := VersionResponse{Running: s.version}
	if onDisk, path := detectUpdate(s.version); onDisk != "" {
		resp.OnDisk = onDisk
		resp.OnDiskPath = path
		resp.UpdateAvailable = true
	}
	writeJSON(w, http.StatusOK, resp)
}
