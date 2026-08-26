package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iml885203/orbit/autoupdate"
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

// candidatePaths returns the daemon's own executable. An update warning must
// point to a binary that restarting this daemon will actually run; scanning
// unrelated installations can create an unrecoverable restart loop.
func candidatePaths() []string {
	if exe, err := os.Executable(); err == nil {
		return filterExistingAndDedup([]string{exe})
	}
	return nil
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

var describedVersionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-(\d+)-g[0-9a-fA-F]+)?(?:-dirty)?$`)

type describedVersion struct {
	major, minor, patch, commits int
}

func parseDescribedVersion(version string) (describedVersion, bool) {
	match := describedVersionPattern.FindStringSubmatch(parseVersionToken(version))
	if match == nil {
		return describedVersion{}, false
	}
	values := [4]int{}
	for i := range values {
		if match[i+1] == "" {
			continue
		}
		value, err := strconv.Atoi(match[i+1])
		if err != nil {
			return describedVersion{}, false
		}
		values[i] = value
	}
	return describedVersion{
		major:   values[0],
		minor:   values[1],
		patch:   values[2],
		commits: values[3],
	}, true
}

func compareDescribedVersions(candidate, running describedVersion) int {
	candidateParts := [...]int{candidate.major, candidate.minor, candidate.patch, candidate.commits}
	runningParts := [...]int{running.major, running.minor, running.patch, running.commits}
	for i := range candidateParts {
		if candidateParts[i] > runningParts[i] {
			return 1
		}
		if candidateParts[i] < runningParts[i] {
			return -1
		}
	}
	return 0
}

func parseVersionTime(version string) (time.Time, bool) {
	open := strings.LastIndex(version, "(")
	close := strings.LastIndex(version, ")")
	if open < 0 || close <= open {
		return time.Time{}, false
	}
	value := strings.TrimSpace(version[open+1 : close])
	for _, layout := range []string{"2006-01-02 15:04:05 -0700", time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func isNewerBuild(candidate, running string) bool {
	candidateToken := parseVersionToken(candidate)
	runningToken := parseVersionToken(running)
	if candidateToken == "" || sameVersion(candidateToken, runningToken) {
		return false
	}

	candidateVersion, candidateOK := parseDescribedVersion(candidateToken)
	runningVersion, runningOK := parseDescribedVersion(runningToken)
	if candidateOK && runningOK {
		if comparison := compareDescribedVersions(candidateVersion, runningVersion); comparison != 0 {
			return comparison > 0
		}
	}

	candidateTime, candidateTimeOK := parseVersionTime(candidate)
	runningTime, runningTimeOK := parseVersionTime(running)
	return candidateTimeOK && runningTimeOK && candidateTime.After(runningTime)
}

// IsNewerBuild reports whether candidate can be ordered after running. CLI
// callers use this to compare the binary the user invoked with the daemon.
func IsNewerBuild(candidate, running string) bool {
	return isNewerBuild(candidate, running)
}

// detectUpdate scans candidate orbit binaries for one Orbit can order after
// the daemon. A different but older or incomparable build must not produce a
// restart recommendation because doing so can silently downgrade the user.
// The daemon's self-exe path is NOT skipped — a user may overwrite that very
// file (install.sh does exactly this), and the on-disk binary's version can
// then meaningfully differ from s.version (the daemon's in-memory build).
func detectUpdate(running string) (onDisk, onDiskPath string) {
	for _, path := range candidatePathsFn() {
		out, err := probeVersion(path)
		if err != nil {
			slog.Debug("version probe failed", "component", "version", "path", path, "err", err)
			continue
		}
		if !isNewerBuild(out, running) {
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
	if launchPath, err := autoupdate.LaunchPath(); err == nil {
		if updateState, stateErr := autoupdate.Load(launchPath); stateErr == nil {
			summary := updateState.Summary()
			resp.ReleaseUpdate = &summary
		}
	}
	if onDisk, path := detectUpdate(s.version); onDisk != "" {
		resp.OnDisk = onDisk
		resp.OnDiskPath = path
		resp.UpdateAvailable = true
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleVersionRestart(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	onDisk, _ := detectUpdate(s.version)
	if onDisk == "" {
		writeJSON(w, http.StatusConflict, APIResponse{Error: "no installed Orbit update is ready"})
		return
	}
	if s.restartLauncher == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{Error: "daemon restart is unavailable in this Orbit build"})
		return
	}
	context := s.environmentContext()
	if err := s.restartLauncher(context.ConfigPath, context.Kind); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: "schedule daemon restart: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, VersionRestartResponse{
		OK:            true,
		Message:       "Orbit is restarting; running resources will be restored",
		TargetVersion: onDisk,
	})
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	launchPath, err := autoupdate.LaunchPath()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}
	state, err := autoupdate.Load(launchPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}
	if state.TargetVersion == "" {
		writeJSON(w, http.StatusConflict, APIResponse{Error: "no verified Orbit update is ready"})
		return
	}
	if s.updateLauncher == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{Error: "Orbit update launcher is unavailable"})
		return
	}
	if err := s.updateLauncher(); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, VersionRestartResponse{
		OK: true, Message: "Orbit update scheduled", TargetVersion: state.TargetVersion,
	})
}

func (s *Server) handleUpdateDrain(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	s.updateAdmissionMu.Lock()
	defer s.updateAdmissionMu.Unlock()
	drained := make(chan struct{})
	go func() {
		s.background.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: "daemon mutations drained"})
	case <-time.After(5 * time.Minute):
		writeJSON(w, http.StatusRequestTimeout, APIResponse{Error: "timed out waiting for daemon mutations to drain", Code: "update_drain_timeout"})
	}
}
