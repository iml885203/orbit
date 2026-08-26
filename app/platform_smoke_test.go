//go:build platformsmoke

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/autoupdate"
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
	if err := os.WriteFile(envPath, []byte("version: \"3\"\n"), 0o600); err != nil {
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
		"init", "--yes", "--source", "smoke", "--path", envRepo, "--env", "quickstart", "--json",
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

func TestPlatformSmokeAutomaticUpdateWorker(t *testing.T) {
	binary := os.Getenv("ORBIT_BIN")
	if binary == "" {
		t.Fatal("ORBIT_BIN is required")
	}
	expectedVersion := os.Getenv("ORBIT_SMOKE_VERSION")
	root := t.TempDir()
	updateHome := filepath.Join(root, "update-home")
	orbitHome := filepath.Join(root, "orbit-home")
	target := filepath.Join(root, "installed", "orbit")
	staged := filepath.Join(updateHome, "updates", "platform-smoke", "orbit")
	if runtime.GOOS == "windows" {
		target += ".exe"
		staged += ".exe"
	}
	for _, path := range []string{target, staged} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ORBIT_UPDATE_HOME", updateHome)
	state, err := autoupdate.Update(target, func(next *autoupdate.State) error {
		next.TargetVersion = expectedVersion
		next.StagedBinary = staged
		next.StagedEvidence = platformSmokeEvidence(t, staged, expectedVersion)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err = autoupdate.BeginTransaction(target, "update", expectedVersion)
	if err != nil {
		t.Fatal(err)
	}
	env := []string{
		"ORBIT_UPDATE_HOME=" + updateHome,
		"ORBIT_HOME=" + orbitHome,
		"ORBIT_INSTALLATION_LAUNCH_PATH=" + target,
		"ORBIT_UPDATE_BACKGROUND=1",
	}
	runPlatformSmokeCommand(t, "", env, binary, "__update-apply", "--operation", "update", "--launch-path", target,
		"--staged", staged, "--installation", state.InstallationID, "--transaction", state.Transaction.ID)
	if output := runPlatformSmokeCommand(t, "", nil, target, "--version"); !strings.Contains(output, expectedVersion) {
		t.Fatalf("updated version = %q, want %q", output, expectedVersion)
	}
	if _, err := os.Stat(target + ".prev"); err != nil {
		t.Fatalf("rollback backup missing: %v", err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("successful update left staged binary: %v", err)
	}

	targetBefore, _ := os.ReadFile(target)
	backupBefore, _ := os.ReadFile(target + ".prev")
	mutated := filepath.Join(updateHome, "updates", "platform-smoke-mutated", filepath.Base(staged))
	if err := os.MkdirAll(filepath.Dir(mutated), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mutated, targetBefore, 0o755); err != nil {
		t.Fatal(err)
	}
	state, err = autoupdate.Update(target, func(next *autoupdate.State) error {
		next.TargetVersion = "v9.9.8"
		next.StagedBinary = mutated
		next.StagedEvidence = platformSmokeEvidence(t, mutated, "v9.9.8")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mutated, []byte("mutated after verification"), 0o755); err != nil {
		t.Fatal(err)
	}
	state, err = autoupdate.BeginTransaction(target, "update", "v9.9.8")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "__update-apply", "--operation", "update", "--launch-path", target,
		"--staged", mutated, "--installation", state.InstallationID, "--transaction", state.Transaction.ID)
	command.Env = append(os.Environ(), env...)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("mutated staged binary succeeded:\n%s", output)
	}
	if after, _ := os.ReadFile(target); string(after) != string(targetBefore) {
		t.Fatal("mutation failure changed installed target")
	}
	if after, _ := os.ReadFile(target + ".prev"); string(after) != string(backupBefore) {
		t.Fatal("mutation failure changed rollback backup")
	}

	bad := filepath.Join(root, "staged", "invalid")
	if runtime.GOOS == "windows" {
		bad += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("not an executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	state, err = autoupdate.Update(target, func(next *autoupdate.State) error {
		next.TargetVersion = "v9.9.9"
		next.StagedBinary = bad
		next.StagedEvidence = platformSmokeEvidence(t, bad, "v9.9.9")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err = autoupdate.BeginTransaction(target, "update", "v9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	command = exec.Command(binary, "__update-apply", "--operation", "update", "--launch-path", target,
		"--staged", bad, "--installation", state.InstallationID, "--transaction", state.Transaction.ID)
	command.Env = append(os.Environ(), env...)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("invalid target succeeded:\n%s", output)
	}
	if output := runPlatformSmokeCommand(t, "", nil, target, "--version"); !strings.Contains(output, expectedVersion) {
		t.Fatalf("rollback version = %q, want %q", output, expectedVersion)
	}
}

func platformSmokeEvidence(t *testing.T, staged, tag string) *autoupdate.VerificationRecord {
	t.Helper()
	contents, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	assetName, err := autoupdate.PlatformAssetName()
	if err != nil {
		t.Fatal(err)
	}
	return &autoupdate.VerificationRecord{
		PolicyVersion: "github-release-v1", Repository: "iml885203/orbit", Tag: tag,
		TargetCommit: strings.Repeat("a", 40), AssetName: assetName,
		AssetSHA256: hex.EncodeToString(digest[:]), ChecksumsSHA256: strings.Repeat("b", 64),
		VerifiedAt: time.Now().UTC(),
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
