package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCheckHostTool_MissingCritical verifies a critical tool absent from PATH
// surfaces as CheckFail with the configured hint.
func TestCheckHostTool_MissingCritical(t *testing.T) {
	t.Setenv("PATH", "")
	c := HostToolCheck{
		Name:     "Docker (test)",
		Binary:   "docker",
		Critical: true,
		Hint:     "Install Docker Desktop or OrbStack",
	}
	got := checkHostTool(c)
	if got.Status != CheckFail {
		t.Errorf("want CheckFail, got %s", got.Status)
	}
	if got.Hint != c.Hint {
		t.Errorf("want hint %q, got %q", c.Hint, got.Hint)
	}
}

// TestCheckHostTool_MissingNonCritical verifies a non-critical tool absent from
// PATH surfaces as CheckWarn (not Fail).
func TestCheckHostTool_MissingNonCritical(t *testing.T) {
	t.Setenv("PATH", "")
	c := HostToolCheck{
		Name:     "git",
		Binary:   "git",
		Critical: false,
		Hint:     "Install git",
	}
	got := checkHostTool(c)
	if got.Status != CheckWarn {
		t.Errorf("want CheckWarn, got %s", got.Status)
	}
	if got.Hint != c.Hint {
		t.Errorf("want hint %q, got %q", c.Hint, got.Hint)
	}
}

// TestCheckHostTool_PresentNoProbe verifies a tool on PATH with no Version
// probe reports pass with the resolved path in the message.
func TestCheckHostTool_PresentNoProbe(t *testing.T) {
	dir, bin := fakeBin(t, "tool-ok", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)
	got := checkHostTool(HostToolCheck{Name: "Tool", Binary: bin, Critical: false})
	if got.Status != CheckPass {
		t.Fatalf("want CheckPass, got %s (%s)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, dir) {
		t.Errorf("want message to include resolved path %q, got %q", dir, got.Message)
	}
}

// TestCheckHostTool_ProbeFailureStillPasses verifies a non-zero exit from the
// version probe does not flip the check to fail — the message should just
// omit the version suffix.
func TestCheckHostTool_ProbeFailureStillPasses(t *testing.T) {
	dir, bin := fakeBin(t, "tool-bad", "#!/bin/sh\nexit 1\n")
	t.Setenv("PATH", dir)
	got := checkHostTool(HostToolCheck{
		Name:    "Tool",
		Binary:  bin,
		Version: versionFromCmd("--version"),
	})
	if got.Status != CheckPass {
		t.Fatalf("probe failure should not flip to %s: %s", got.Status, got.Message)
	}
	if strings.Contains(got.Message, "(") {
		t.Errorf("probe failed but message contains version-like suffix: %q", got.Message)
	}
}

// TestCheckHostTool_ProbeSuccess verifies a successful version probe appends
// the first output line to the pass message.
func TestCheckHostTool_ProbeSuccess(t *testing.T) {
	dir, bin := fakeBin(t, "tool-ver", "#!/bin/sh\necho 'fake version 1.2.3'\n")
	t.Setenv("PATH", dir)
	got := checkHostTool(HostToolCheck{
		Name:    "Tool",
		Binary:  bin,
		Version: versionFromCmd("--version"),
	})
	if got.Status != CheckPass {
		t.Fatalf("want CheckPass, got %s", got.Status)
	}
	if !strings.Contains(got.Message, "fake version 1.2.3") {
		t.Errorf("want version in message, got %q", got.Message)
	}
}

// TestDotnetSDKVersion_EmptyIsError verifies --list-sdks returning empty is
// treated as a probe error (runtime-only install) so callers can warn.
func TestDotnetSDKVersion_EmptyIsError(t *testing.T) {
	dir, bin := fakeBin(t, "dotnet-empty", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)
	path := filepath.Join(dir, bin)
	if _, err := dotnetSDKVersion(path); err == nil {
		t.Error("want error for empty --list-sdks output, got nil")
	}
}

func TestCheckNode_MissingWarns(t *testing.T) {
	t.Setenv("PATH", "")
	got := checkNode()
	if got.Status != CheckWarn {
		t.Errorf("want CheckWarn, got %s", got.Status)
	}
}

func TestCheckNode_OldVersionWarns(t *testing.T) {
	dir, _ := fakeBin(t, "node", "#!/bin/sh\necho 'v18.0.0'\n")
	t.Setenv("PATH", dir)
	got := checkNode()
	if got.Status != CheckWarn {
		t.Fatalf("want CheckWarn for old version, got %s (%s)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "v18") {
		t.Errorf("want version in message, got %q", got.Message)
	}
}

func TestCheckNode_ModernVersionPasses(t *testing.T) {
	dir, _ := fakeBin(t, "node", "#!/bin/sh\necho 'v22.3.1'\n")
	t.Setenv("PATH", dir)
	got := checkNode()
	if got.Status != CheckPass {
		t.Fatalf("want CheckPass, got %s (%s)", got.Status, got.Message)
	}
}

func TestParseNodeMajor(t *testing.T) {
	cases := map[string]int{"v22.3.1": 22, "v18.0.0": 18, "": 0, "garbage": 0, "v20": 0}
	for in, want := range cases {
		if got := parseNodeMajor(in); got != want {
			t.Errorf("parseNodeMajor(%q) = %d, want %d", in, got, want)
		}
	}
}

// fakeBin writes an executable script to a temp dir and returns (dir, name).
func fakeBin(t *testing.T, name, body string) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell scripts don't work on windows")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, name
}
