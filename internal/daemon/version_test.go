package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseVersionToken(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"dirty with time", "c9c714b-dirty (2026-04-19 20:03:47 +0800)", "c9c714b-dirty"},
		{"release tag with time", "v1.2.3 (2026-04-19 20:03:47 +0800)", "v1.2.3"},
		{"unknown", "unknown", "unknown"},
		{"empty", "", ""},
		{"leading whitespace", "  c9c714b (2026-04-19 20:03:47 +0800)", "c9c714b"},
		{"trailing whitespace", "c9c714b\n", "c9c714b"},
		{"tab separator", "abc123\t(2026-04-19 20:03:47 +0800)", "abc123"},
		{"hash only", "c9c714b", "c9c714b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseVersionToken(tc.in); got != tc.want {
				t.Errorf("parseVersionToken(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSameVersion(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"dirty same hash suppressed", "abc-dirty", "abc-dirty", true},
		{"dirty different hash reported", "abc-dirty", "def-dirty", false},
		{"dirty vs clean same hash reported", "abc-dirty", "abc", false},
		{"clean vs clean same", "abc", "abc", true},
		{"clean vs clean different", "abc", "def", false},
		{"empty vs empty", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameVersion(tc.a, tc.b); got != tc.want {
				t.Errorf("sameVersion(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestIsNewerBuildUnderstandsGitDescribeVersions(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		running   string
		want      bool
	}{
		{
			name:      "installed release is older than source main",
			candidate: "v0.0.4 (2026-07-27 12:44:56 +0800)",
			running:   "v0.0.4-18-g39fb358 (2026-07-28 11:00:00 +0800)",
			want:      false,
		},
		{
			name:      "source main is newer than installed release",
			candidate: "v0.0.4-18-g39fb358 (2026-07-28 11:00:00 +0800)",
			running:   "v0.0.4 (2026-07-27 12:44:56 +0800)",
			want:      true,
		},
		{
			name:      "next release is newer than source main",
			candidate: "v0.0.5 (2026-07-29 12:00:00 +0800)",
			running:   "v0.0.4-18-g39fb358 (2026-07-28 11:00:00 +0800)",
			want:      true,
		},
		{
			name:      "older described commit is not newer despite later build time",
			candidate: "v0.0.4-10-g1111111 (2026-07-29 12:00:00 +0800)",
			running:   "v0.0.4-18-g39fb358 (2026-07-28 11:00:00 +0800)",
			want:      false,
		},
		{
			name:      "unknown hashes use build time",
			candidate: "def456 (2026-07-29 12:00:00 +0800)",
			running:   "abc123 (2026-07-28 11:00:00 +0800)",
			want:      true,
		},
		{
			name:      "incomparable builds do not recommend restart",
			candidate: "def456",
			running:   "abc123",
			want:      false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isNewerBuild(test.candidate, test.running); got != test.want {
				t.Fatalf("isNewerBuild(%q, %q) = %v, want %v", test.candidate, test.running, got, test.want)
			}
		})
	}
}

// withProbe swaps probeVersion for the duration of a test.
func withProbe(t *testing.T, fn func(path string) (string, error)) {
	t.Helper()
	orig := probeVersion
	probeVersion = fn
	t.Cleanup(func() { probeVersion = orig })
}

// makeBin creates an executable file at dir/name and returns its path.
func makeBin(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDetectUpdateDirtySameHashSuppressed(t *testing.T) {
	dir := t.TempDir()
	bin := makeBin(t, dir, "orbit")
	withProbe(t, func(path string) (string, error) {
		return "abc-dirty (2026-04-19 20:03:47 +0800)", nil
	})
	withCandidates(t, []string{bin})

	onDisk, _ := detectUpdate("abc-dirty (2026-04-19 19:00:00 +0800)")
	if onDisk != "" {
		t.Errorf("expected no update for same dirty hash, got %q", onDisk)
	}
}

func TestDetectUpdateDirtyDifferentHashReported(t *testing.T) {
	dir := t.TempDir()
	bin := makeBin(t, dir, "orbit")
	withProbe(t, func(path string) (string, error) {
		return "def-dirty (2026-04-19 20:03:47 +0800)", nil
	})
	withCandidates(t, []string{bin})

	onDisk, onPath := detectUpdate("abc-dirty (2026-04-19 19:00:00 +0800)")
	if onDisk == "" {
		t.Fatal("expected update, got none")
	}
	if onPath != bin {
		t.Errorf("expected path %q, got %q", bin, onPath)
	}
}

func TestDetectUpdateDirtyVsClean(t *testing.T) {
	dir := t.TempDir()
	bin := makeBin(t, dir, "orbit")
	withProbe(t, func(path string) (string, error) {
		return "abc (2026-04-19 20:03:47 +0800)", nil
	})
	withCandidates(t, []string{bin})

	onDisk, _ := detectUpdate("abc-dirty (2026-04-19 19:00:00 +0800)")
	if onDisk == "" {
		t.Error("expected update when going from dirty to clean")
	}
}

func TestDetectUpdateSkipsOlderReleaseBesideSourceBuild(t *testing.T) {
	dir := t.TempDir()
	self := makeBin(t, dir, "source-orbit")
	installed := makeBin(t, dir, "installed-orbit")
	withProbe(t, func(path string) (string, error) {
		switch path {
		case self:
			return "v0.0.4-18-g39fb358 (2026-07-28 11:00:00 +0800)", nil
		case installed:
			return "v0.0.4 (2026-07-27 12:44:56 +0800)", nil
		default:
			t.Fatalf("unexpected probe path %q", path)
			return "", nil
		}
	})
	withCandidates(t, []string{self, installed})

	onDisk, onDiskPath := detectUpdate("v0.0.4-18-g39fb358 (2026-07-28 11:00:00 +0800)")
	if onDisk != "" || onDiskPath != "" {
		t.Fatalf("older installed release reported as update: version=%q path=%q", onDisk, onDiskPath)
	}
}

func TestDetectUpdateDedupeCandidates(t *testing.T) {
	dir := t.TempDir()
	target := makeBin(t, dir, "orbit-target")
	linkA := filepath.Join(dir, "linkA")
	linkB := filepath.Join(dir, "linkB")
	if err := os.Symlink(target, linkA); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, linkB); err != nil {
		t.Fatal(err)
	}
	calls := 0
	withProbe(t, func(path string) (string, error) {
		calls++
		return "def (2026-04-19 20:03:47 +0800)", nil
	})
	withCandidates(t, []string{linkA, linkB})

	_, _ = detectUpdate("abc (2026-04-19 19:00:00 +0800)")
	if calls != 1 {
		t.Errorf("expected 1 probe call after dedup, got %d", calls)
	}
}

// TestDetectUpdateSelfExeOverwritten covers the install.sh case: the daemon's
// own executable path gets overwritten with a newer binary. The on-disk
// version then differs from s.version (the in-memory build), and we must
// report it — otherwise the user never sees "restart to pick up".
func TestDetectUpdateSelfExeOverwritten(t *testing.T) {
	dir := t.TempDir()
	self := makeBin(t, dir, "orbit-self")
	withProbe(t, func(path string) (string, error) {
		return "def (2026-04-19 20:03:47 +0800)", nil
	})
	withCandidates(t, []string{self})

	onDisk, onDiskPath := detectUpdate("abc (2026-04-19 19:00:00 +0800)")
	if onDisk == "" {
		t.Error("expected update when self-exe on disk differs from running version")
	}
	if onDiskPath != self {
		t.Errorf("expected path %q, got %q", self, onDiskPath)
	}
}

// --- test helpers that swap internals ---

func withCandidates(t *testing.T, paths []string) {
	t.Helper()
	orig := candidatePathsFn
	candidatePathsFn = func() []string { return filterExistingAndDedup(paths) }
	t.Cleanup(func() { candidatePathsFn = orig })
}
