package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/shellquote"
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

func TestCheckNode_MissingFailsWhenRequired(t *testing.T) {
	t.Setenv("PATH", "")
	got := checkHostTool(hostToolDefinition("node", []string{"web"}))
	if got.Status != CheckFail {
		t.Errorf("want CheckFail, got %s", got.Status)
	}
	if !strings.Contains(got.Message, "required by web") {
		t.Errorf("want service context, got %q", got.Message)
	}
}

func TestCheckNode_ProjectOwnsVersionPolicy(t *testing.T) {
	dir, _ := fakeBin(t, "node", "#!/bin/sh\necho 'v18.0.0'\n")
	t.Setenv("PATH", dir)
	got := checkHostTool(hostToolDefinition("node", []string{"web"}))
	if got.Status != CheckPass {
		t.Fatalf("Orbit should not override the project's version policy, got %s (%s)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "v18") {
		t.Errorf("want version in message, got %q", got.Message)
	}
}

func TestCheckNode_ModernVersionPasses(t *testing.T) {
	dir, _ := fakeBin(t, "node", "#!/bin/sh\necho 'v22.3.1'\n")
	t.Setenv("PATH", dir)
	got := checkHostTool(hostToolDefinition("node", []string{"web"}))
	if got.Status != CheckPass {
		t.Fatalf("want CheckPass, got %s (%s)", got.Status, got.Message)
	}
}

func TestRequiredHostTools_OnlyReportsSelectedEnvironment(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{
		"demo-api": {Type: "python", Command: "python3 -m http.server 8080"},
	}}

	tools := requiredHostTools(cfg)
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
		if tool.Binary == "python3" {
			if len(tool.RequiredBy) != 1 || tool.RequiredBy[0] != "demo-api" {
				t.Errorf("Python required by = %v, want [demo-api]", tool.RequiredBy)
			}
		}
	}
	if got := strings.Join(names, ","); got != "Git,Python" {
		t.Errorf("tools = %s, want Git,Python", got)
	}
}

func TestNodeProjectDependencyCheckDistinguishesMissingPackages(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"scripts":{"dev":"vite"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := nodeProjectDependencyCheck("web", project, "pnpm")
	if got.Status != CheckFail {
		t.Fatalf("status = %s, want fail: %+v", got.Status, got)
	}
	if got.Message != "project packages are not installed" {
		t.Fatalf("message = %q", got.Message)
	}
	want := "run: pnpm --dir " + shellquote.Quote(project) + " install"
	if got.Hint != want {
		t.Fatalf("hint = %q, want %q", got.Hint, want)
	}
}

func TestNodeProjectDependencyCheckPassesInstalledPackages(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := nodeProjectDependencyCheck("web", project, "npm")
	if got.Status != CheckPass {
		t.Fatalf("status = %s, want pass: %+v", got.Status, got)
	}
}

func TestNodeProjectDependencyCheckAcceptsYarnPnP(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".pnp.cjs"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := nodeProjectDependencyCheck("web", project, "yarn")
	if got.Status != CheckPass {
		t.Fatalf("status = %s, want pass: %+v", got.Status, got)
	}
}

func TestProjectDependencyChecksOnlyInspectPackageManagerServices(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Services: map[string]*config.Service{
		"app":    {Type: "node", Command: "pnpm dev", Path: project},
		"script": {Type: "node", Command: "node index.js", Path: project},
		"api":    {Type: "python", Command: "python3 app.py", Path: project},
	}}
	checks := projectDependencyChecks(cfg)
	if len(checks) != 1 || checks[0].Name != "Packages (app)" {
		t.Fatalf("checks = %+v", checks)
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
