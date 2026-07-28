//go:build platformsmoke

package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformSmokeCleanUserJourney(t *testing.T) {
	binary := os.Getenv("ORBIT_BIN")
	if binary == "" {
		t.Fatal("ORBIT_BIN is required")
	}
	expectedVersion := os.Getenv("ORBIT_SMOKE_VERSION")
	if expectedVersion == "" {
		t.Fatal("ORBIT_SMOKE_VERSION is required")
	}

	version := runPlatformSmokeCommand(t, "", nil, binary, "--version")
	if !strings.Contains(version, expectedVersion) {
		t.Fatalf("version output %q does not contain %q", version, expectedVersion)
	}
	versionCommand := runPlatformSmokeCommand(t, "", nil, binary, "version")
	if versionCommand != version {
		t.Fatalf("version command output %q does not match --version output %q", versionCommand, version)
	}

	root := t.TempDir()
	envRepo := filepath.Join(root, "environment repo")
	workspace := filepath.Join(root, "workspace with spaces")
	orbitHome := filepath.Join(root, "orbit home")
	for _, dir := range []string{envRepo, workspace, orbitHome} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	envsDir := filepath.Join(envRepo, "envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(envsDir, "quickstart.yaml")
	if err := os.WriteFile(envPath, []byte("version: \"2\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runPlatformSmokeCommand(t, envRepo, nil, "git", "init")
	runPlatformSmokeCommand(t, envRepo, nil, "git", "config", "user.email", "smoke@orbit.invalid")
	runPlatformSmokeCommand(t, envRepo, nil, "git", "config", "user.name", "Orbit Smoke")
	runPlatformSmokeCommand(t, envRepo, nil, "git", "add", "envs/quickstart.yaml")
	runPlatformSmokeCommand(t, envRepo, nil, "git", "commit", "-m", "smoke environment")

	commandEnv := []string{"ORBIT_HOME=" + orbitHome}
	initOutput := runPlatformSmokeCommand(
		t,
		workspace,
		commandEnv,
		binary,
		"init", "--yes", "--env-repo", envRepo, "--env", "quickstart", "--json",
	)
	initEnvelope := decodePlatformSmokeEnvelope(t, initOutput)
	if !initEnvelope.OK {
		t.Fatalf("init failed: %+v\n%s", initEnvelope.Error, initOutput)
	}

	statusOutput := runPlatformSmokeCommand(t, workspace, commandEnv, binary, "status", "--json")
	statusEnvelope := decodePlatformSmokeEnvelope(t, statusOutput)
	if !statusEnvelope.OK {
		t.Fatalf("status failed: %+v\n%s", statusEnvelope.Error, statusOutput)
	}
	var statusData struct {
		Daemon struct {
			Running bool `json:"running"`
		} `json:"daemon"`
		Resources []json.RawMessage `json:"resources"`
	}
	if err := json.Unmarshal(statusEnvelope.Data, &statusData); err != nil {
		t.Fatalf("status data: %v\n%s", err, statusOutput)
	}
	if statusData.Daemon.Running {
		t.Fatal("clean-user status unexpectedly reports a running daemon")
	}
	if statusData.Resources == nil || len(statusData.Resources) != 0 {
		t.Fatalf("resources must be an empty array, got %s", statusEnvelope.Data)
	}

	doctorOutput := runPlatformSmokeCommand(t, workspace, commandEnv, binary, "doctor", "--json")
	doctorEnvelope := decodePlatformSmokeEnvelope(t, doctorOutput)
	if !doctorEnvelope.OK {
		t.Fatalf("empty environment doctor failed: %+v\n%s", doctorEnvelope.Error, doctorOutput)
	}
}

type platformSmokeEnvelope struct {
	SchemaVersion string          `json:"schema_version"`
	OK            bool            `json:"ok"`
	Data          json.RawMessage `json:"data"`
	Error         *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func decodePlatformSmokeEnvelope(t *testing.T, output string) platformSmokeEnvelope {
	t.Helper()
	var envelope platformSmokeEnvelope
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, output)
	}
	if envelope.SchemaVersion != "orbit.cli.v1" {
		t.Fatalf("schema_version = %q\n%s", envelope.SchemaVersion, output)
	}
	return envelope
}

func runPlatformSmokeCommand(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	if dir != "" {
		command.Dir = dir
	}
	command.Env = append(os.Environ(), env...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}
