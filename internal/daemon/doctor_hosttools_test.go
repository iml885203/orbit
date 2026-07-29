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
	t.Parallel()
	dir, bin := fakeBin(t, "tool-bad", "#!/bin/sh\nexit 1\n")
	got := checkHostTool(HostToolCheck{
		Name:    "Tool",
		Binary:  filepath.Join(dir, bin),
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
	t.Parallel()
	dir, bin := fakeBin(t, "tool-ver", "#!/bin/sh\necho 'fake version 1.2.3'\n")
	got := checkHostTool(HostToolCheck{
		Name:    "Tool",
		Binary:  filepath.Join(dir, bin),
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
	t.Parallel()
	dir, bin := fakeBin(t, "dotnet-empty", "#!/bin/sh\nexit 0\n")
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
	t.Parallel()
	dir, _ := fakeBin(t, "node", "#!/bin/sh\necho 'v18.0.0'\n")
	tool := hostToolDefinition("node", []string{"web"})
	tool.Binary = filepath.Join(dir, "node")
	got := checkHostTool(tool)
	if got.Status != CheckPass {
		t.Fatalf("Orbit should not override the project's version policy, got %s (%s)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "v18") {
		t.Errorf("want version in message, got %q", got.Message)
	}
}

func TestCheckNode_ModernVersionPasses(t *testing.T) {
	t.Parallel()
	dir, _ := fakeBin(t, "node", "#!/bin/sh\necho 'v22.3.1'\n")
	tool := hostToolDefinition("node", []string{"web"})
	tool.Binary = filepath.Join(dir, "node")
	got := checkHostTool(tool)
	if got.Status != CheckPass {
		t.Fatalf("want CheckPass, got %s (%s)", got.Status, got.Message)
	}
}

func TestRequiredHostTools_OnlyReportsSelectedEnvironment(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{
		"demo-api": {Type: "python", Command: "python3 -m http.server 8080"},
		"worker":   {Type: "go", Command: "go run ."},
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
	if got := strings.Join(names, ","); got != "Go,Python" {
		t.Errorf("tools = %s, want Go,Python", got)
	}
}

func TestHostEnvironmentChecksReportsMissingGoRuntime(t *testing.T) {
	t.Setenv("PATH", "")
	project := t.TempDir()
	cfg := &config.Config{Services: map[string]*config.Service{
		"worker": {Type: "go", Path: project, Command: "go run ."},
	}}

	checks := HostEnvironmentChecks(cfg)
	if len(checks) != 1 {
		t.Fatalf("checks = %+v", checks)
	}
	if checks[0].Name != "Go" || checks[0].Status != CheckFail {
		t.Fatalf("Go check = %+v", checks[0])
	}
	if !strings.Contains(checks[0].Hint, "go.dev/doc/install") {
		t.Fatalf("hint = %q", checks[0].Hint)
	}
}

func TestNodeProjectDependencyCheckDistinguishesMissingPackages(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"scripts":{"dev":"vite"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := nodeProjectDependencyCheck("web", project, project, "pnpm")
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
	got := nodeProjectDependencyCheck("web", project, project, "npm")
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
	got := nodeProjectDependencyCheck("web", project, project, "yarn")
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

func TestDirectNodeServiceUsesWorkspacePackageManager(t *testing.T) {
	runtimeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(runtimeDir, "pnpm"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workspace, "package.json"),
		[]byte(`{"packageManager":"pnpm@10.0.0"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	servicePath := filepath.Join(workspace, "apps", "api")
	if err := os.MkdirAll(servicePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(servicePath, "package.json"),
		[]byte(`{"dependencies":{"fastify":"5.0.0"}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "node", Command: "node server.js", Path: servicePath},
	}}

	checks := projectDependencyChecks(cfg)
	if len(checks) != 1 || checks[0].Status != CheckFail {
		t.Fatalf("checks = %+v", checks)
	}
	want := "run: pnpm --dir " + shellquote.Quote(workspace) + " install"
	if checks[0].Hint != want {
		t.Fatalf("hint = %q, want %q", checks[0].Hint, want)
	}
	if command, ok := ProjectDependencySetupCommand(cfg, "api"); !ok ||
		command != "pnpm --dir "+shellquote.Quote(workspace)+" install" {
		t.Fatalf("setup command = %q, %v", command, ok)
	}
	tools := requiredHostTools(cfg)
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	if got := strings.Join(names, ","); got != "Node.js,pnpm" {
		t.Fatalf("tools = %s, want Node.js,pnpm", got)
	}

	if err := os.Mkdir(filepath.Join(workspace, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	checks = projectDependencyChecks(cfg)
	if len(checks) != 1 || checks[0].Status != CheckPass {
		t.Fatalf("installed checks = %+v", checks)
	}
	if command, ok := ProjectDependencySetupCommand(cfg, "api"); ok || command != "" {
		t.Fatalf("unexpected setup command = %q, %v", command, ok)
	}
}

func TestProjectDependencyChecksCoalesceOneWorkspaceSetup(t *testing.T) {
	runtimeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(runtimeDir, "pnpm"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workspace, "package.json"),
		[]byte(`{"packageManager":"pnpm@10.0.0","dependencies":{"shared":"1.0.0"}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	services := map[string]*config.Service{}
	for _, name := range []string{"api", "worker"} {
		path := filepath.Join(workspace, "apps", name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(path, "package.json"),
			[]byte(`{"dependencies":{"shared":"1.0.0"}}`),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
		services[name] = &config.Service{Type: "node", Command: "node server.js", Path: path}
	}

	checks := projectDependencyChecks(&config.Config{Services: services})

	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want one shared setup", checks)
	}
	check := checks[0]
	if check.Name != "Packages (api, worker)" {
		t.Fatalf("name = %q", check.Name)
	}
	if check.Message != "project packages are not installed (required by api, worker)" {
		t.Fatalf("message = %q", check.Message)
	}
	wantHint := "run: pnpm --dir " + shellquote.Quote(workspace) + " install"
	if check.Hint != wantHint {
		t.Fatalf("hint = %q, want %q", check.Hint, wantHint)
	}
}

func TestMissingNodePackageManagerDefersPackageInstall(t *testing.T) {
	runtimeDir, _ := fakeBin(t, "node", "#!/bin/sh\necho v22.0.0\n")
	t.Setenv("PATH", runtimeDir)
	project := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(project, "package.json"),
		[]byte(`{"packageManager":"pnpm@10.0.0","dependencies":{"fastify":"5.0.0"}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "node", Command: "node server.js", Path: project},
	}}

	checks := HostEnvironmentChecks(cfg)
	var failedNames []string
	for _, check := range checks {
		if check.Status == CheckFail {
			failedNames = append(failedNames, check.Name)
		}
		if strings.HasPrefix(check.Name, "Packages (") {
			t.Fatalf("package install was suggested before pnpm is available: %+v", checks)
		}
	}
	if got := strings.Join(failedNames, ","); got != "pnpm" {
		t.Fatalf("failed checks = %s, want pnpm; all checks = %+v", got, checks)
	}
	if command, ok := ProjectDependencySetupCommand(cfg, "api"); ok || command != "" {
		t.Fatalf("unrunnable setup command = %q, %v", command, ok)
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
