package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestHostEnvironmentChecksNodeVersionMatchesProject(t *testing.T) {
	project := t.TempDir()
	writeRuntimeFile(t, project, ".nvmrc", "22\n")
	dir, _ := fakeBin(t, "node", "#!/bin/sh\necho 'v22.4.1'\n")
	t.Setenv("PATH", dir)

	check := runtimeCheck(t, HostEnvironmentChecks(nodeConfig(project)), "Node.js")
	if check.Status != CheckPass {
		t.Fatalf("check = %+v", check)
	}
	if !strings.Contains(check.Message, "matches web requires 22 (.nvmrc)") {
		t.Fatalf("message = %q", check.Message)
	}
}

func TestHostEnvironmentChecksNodeVersionMismatchHasOneRuntimeConclusion(t *testing.T) {
	project := t.TempDir()
	writeRuntimeFile(t, project, ".node-version", "20.11.1\n")
	dir, _ := fakeBin(t, "node", "#!/bin/sh\necho 'v22.4.1'\n")
	t.Setenv("PATH", dir)

	checks := HostEnvironmentChecks(nodeConfig(project))
	check := runtimeCheck(t, checks, "Node.js")
	if check.Status != CheckFail {
		t.Fatalf("check = %+v", check)
	}
	if !strings.Contains(check.Message, "web requires 20.11.1 (.node-version); installed 22.4.1") {
		t.Fatalf("message = %q", check.Message)
	}
	if !strings.Contains(check.Hint, project) || !strings.Contains(check.Hint, "Select the project version of Node.js") {
		t.Fatalf("hint = %q", check.Hint)
	}
	count := 0
	for _, candidate := range checks {
		if candidate.Name == "Node.js" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("Node.js checks = %d, want one: %+v", count, checks)
	}
}

func TestHostEnvironmentChecksMissingNodeNamesRequiredVersion(t *testing.T) {
	project := t.TempDir()
	writeRuntimeFile(t, project, ".nvmrc", "20\n")
	t.Setenv("PATH", "")

	check := runtimeCheck(t, HostEnvironmentChecks(nodeConfig(project)), "Node.js")
	if check.Status != CheckFail {
		t.Fatalf("check = %+v", check)
	}
	if !strings.Contains(check.Hint, "web requires 20 (.nvmrc)") {
		t.Fatalf("hint = %q", check.Hint)
	}
}

func TestHostEnvironmentChecksNodeAliasIsHonestWarning(t *testing.T) {
	project := t.TempDir()
	writeRuntimeFile(t, project, ".nvmrc", "lts/*\n")
	dir, _ := fakeBin(t, "node", "#!/bin/sh\necho 'v22.4.1'\n")
	t.Setenv("PATH", dir)

	check := runtimeCheck(t, HostEnvironmentChecks(nodeConfig(project)), "Node.js")
	if check.Status != CheckWarn {
		t.Fatalf("check = %+v", check)
	}
	if !strings.Contains(check.Message, "cannot verify version alias") {
		t.Fatalf("message = %q", check.Message)
	}
}

func TestHostEnvironmentChecksConflictingNodeFilesFail(t *testing.T) {
	project := t.TempDir()
	writeRuntimeFile(t, project, ".nvmrc", "20\n")
	writeRuntimeFile(t, project, ".node-version", "22\n")
	dir, _ := fakeBin(t, "node", "#!/bin/sh\necho 'v22.4.1'\n")
	t.Setenv("PATH", dir)

	check := runtimeCheck(t, HostEnvironmentChecks(nodeConfig(project)), "Node.js")
	if check.Status != CheckFail {
		t.Fatalf("check = %+v", check)
	}
	if !strings.Contains(check.Message, "requires 20 (.nvmrc)") ||
		!strings.Contains(check.Message, "requires 22 (.node-version)") {
		t.Fatalf("message does not expose both declarations: %q", check.Message)
	}
	if !strings.Contains(check.Hint, "Align the conflicting Node.js version files") {
		t.Fatalf("hint = %q", check.Hint)
	}
}

func TestHostEnvironmentChecksPythonVersionFromToolVersions(t *testing.T) {
	project := t.TempDir()
	writeRuntimeFile(t, project, ".tool-versions", "nodejs 22.3.0\npython 3.12\n")
	dir, _ := fakeBin(t, "python3", "#!/bin/sh\necho 'Python 3.12.4'\n")
	t.Setenv("PATH", dir)
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "python", Command: "python3 app.py", Path: project},
	}}

	check := runtimeCheck(t, HostEnvironmentChecks(cfg), "Python")
	if check.Status != CheckPass || !strings.Contains(check.Message, "api requires 3.12 (.tool-versions)") {
		t.Fatalf("check = %+v", check)
	}
}

func TestHostEnvironmentChecksGoModToolchainBeforeStartup(t *testing.T) {
	project := t.TempDir()
	writeRuntimeFile(t, project, "go.mod", "module example.invalid/api\n\ngo 1.24\ntoolchain go1.25.1\n")
	dir, _ := fakeBin(t, "go", "#!/bin/sh\necho 'go version go1.24.6 test/arch'\n")
	t.Setenv("PATH", dir)
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "go", Command: "go run .", Path: project},
	}}

	check := runtimeCheck(t, HostEnvironmentChecks(cfg), "Go")
	if check.Status != CheckFail {
		t.Fatalf("check = %+v", check)
	}
	if !strings.Contains(check.Message, "api requires 1.25.1 (go.mod); installed 1.24.6") {
		t.Fatalf("message = %q", check.Message)
	}
	if !strings.Contains(check.Hint, "Select the project version of Go") {
		t.Fatalf("hint = %q", check.Hint)
	}
}

func TestHostEnvironmentChecksMalformedVersionFileFails(t *testing.T) {
	project := t.TempDir()
	writeRuntimeFile(t, project, ".python-version", "\n")
	dir, _ := fakeBin(t, "python3", "#!/bin/sh\necho 'Python 3.12.4'\n")
	t.Setenv("PATH", dir)
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "python", Command: "python3 app.py", Path: project},
	}}

	check := runtimeCheck(t, HostEnvironmentChecks(cfg), "Python")
	if check.Status != CheckFail || !strings.Contains(check.Message, "file is empty") {
		t.Fatalf("check = %+v", check)
	}
}

func TestHostEnvironmentChecksMissingRuntimeExposesMalformedVersionFile(t *testing.T) {
	project := t.TempDir()
	writeRuntimeFile(t, project, ".python-version", "\n")
	t.Setenv("PATH", "")
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "python", Command: "python3 app.py", Path: project},
	}}

	check := runtimeCheck(t, HostEnvironmentChecks(cfg), "Python")
	if check.Status != CheckFail {
		t.Fatalf("check = %+v", check)
	}
	if !strings.Contains(check.Hint, ".python-version: file is empty") {
		t.Fatalf("hint = %q", check.Hint)
	}
}

func TestHostEnvironmentChecksBunDoesNotRequireNode(t *testing.T) {
	project := t.TempDir()
	writeRuntimeFile(t, project, ".bun-version", "1.2\n")
	dir, _ := fakeBin(t, "bun", "#!/bin/sh\necho '1.2.7'\n")
	t.Setenv("PATH", dir)
	cfg := &config.Config{Services: map[string]*config.Service{
		"web": {Type: "node", Command: "bun run dev", Path: project},
	}}

	checks := HostEnvironmentChecks(cfg)
	check := runtimeCheck(t, checks, "Bun")
	if check.Status != CheckPass || !strings.Contains(check.Message, "web requires 1.2 (.bun-version)") {
		t.Fatalf("Bun check = %+v", check)
	}
	for _, candidate := range checks {
		if candidate.Name == "Node.js" {
			t.Fatalf("Bun service should not require Node.js: %+v", checks)
		}
	}
}

func TestHostEnvironmentChecksDotnetLetsSDKResolverApplyGlobalJSON(t *testing.T) {
	project := t.TempDir()
	writeRuntimeFile(t, project, "global.json", `{"sdk":{"version":"8.0.100","rollForward":"latestPatch"}}`)
	dir, _ := fakeBin(t, "dotnet", "#!/bin/sh\necho '8.0.104'\n")
	t.Setenv("PATH", dir)
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {Type: "dotnet", Command: "dotnet run", Path: project},
	}}

	check := runtimeCheck(t, HostEnvironmentChecks(cfg), ".NET SDK")
	if check.Status != CheckPass {
		t.Fatalf("check = %+v", check)
	}
	if !strings.Contains(check.Message, "api requires 8.0.100 (global.json) resolves to 8.0.104") {
		t.Fatalf("message = %q", check.Message)
	}
}

func TestCompatibleVersionUsesDeclaredPrecision(t *testing.T) {
	tests := []struct {
		installed  string
		requested  string
		compatible bool
		comparable bool
	}{
		{installed: "v22.4.1", requested: "22", compatible: true, comparable: true},
		{installed: "Python 3.12.4", requested: "3.12", compatible: true, comparable: true},
		{installed: "v22.4.1", requested: "20", compatible: false, comparable: true},
		{installed: "v22.4.1", requested: "lts/*", compatible: false, comparable: false},
	}
	for _, test := range tests {
		compatible, comparable := compatibleVersion(test.installed, test.requested)
		if compatible != test.compatible || comparable != test.comparable {
			t.Errorf("compatibleVersion(%q, %q) = (%v, %v)", test.installed, test.requested, compatible, comparable)
		}
	}
}

func TestGoVersionRequirementAcceptsNewerToolchain(t *testing.T) {
	tests := []struct {
		installed  string
		requested  string
		compatible bool
	}{
		{installed: "go1.26.5", requested: "1.24", compatible: true},
		{installed: "go1.24.2", requested: "1.24", compatible: true},
		{installed: "go1.23.9", requested: "1.24", compatible: false},
	}
	for _, test := range tests {
		compatible, comparable := minimumVersion(test.installed, test.requested)
		if !comparable || compatible != test.compatible {
			t.Errorf("minimumVersion(%q, %q) = (%v, %v)", test.installed, test.requested, compatible, comparable)
		}
	}
}

func nodeConfig(project string) *config.Config {
	return &config.Config{Services: map[string]*config.Service{
		"web": {Type: "node", Command: "node server.js", Path: project},
	}}
}

func runtimeCheck(t *testing.T, checks []DoctorCheck, name string) DoctorCheck {
	t.Helper()
	for _, check := range checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("%s not found in %+v", name, checks)
	return DoctorCheck{}
}

func writeRuntimeFile(t *testing.T, project, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(project, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
