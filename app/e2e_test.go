//go:build e2e

package app

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/cli"
)

// These tests exercise the real orbit binary against a real Docker engine.
// They are gated behind `//go:build e2e`. Run with:
//
//	make test-e2e      # or: go test -tags=e2e -v ./app/
//
// Every test gets its own isolated instance:
//   - ORBIT_HOME → unique tmp dir (socket, pid, state, env config)
//   - ORBIT_NAMESPACE → random hex (Docker container names + labels)
//   - ORBIT_DASHBOARD_PORT → random high port (TCP listener)
// This means e2e can run alongside the developer's regular orbit daemon and
// workload without interfering. Containers are cleaned up via `orbit down`.
//
// Requirements: Docker running. The test harness uses the binary pointed
// at by `ORBIT_BIN` (set by `make test-e2e`), falling back to PATH.

const (
	e2eBootTimeout  = 30 * time.Second
	e2eReadyTimeout = 60 * time.Second
)

type e2eEnv struct {
	home      string
	binary    string
	envYaml   string
	namespace string
	port      int
}

func setupE2E(t *testing.T) *e2eEnv {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	binary := findOrbitBinary(t)

	// macOS unix sockets have a 104-char path limit. t.TempDir() produces
	// paths like /var/folders/../T/<TestName>12345/001 that easily exceed
	// this. Use a short stable prefix in /tmp instead.
	home, err := os.MkdirTemp("/tmp", "orb-")
	if err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Logf("ORBIT_HOME=%s", home)

	// `orbit up` now runs a preflight that requires ~/.orbit/envs to exist
	// and be non-empty. Seed it from testdata/ so we don't depend on envs/
	// (which is the user-facing env repo contents, not a test fixture).
	srcEnv := findRepoFile(t, filepath.Join("cmd", "orbit", "testdata", "e2e-minimal.yaml"))
	envsDir := filepath.Join(home, "envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatalf("mkdir envs: %v", err)
	}
	data, err := os.ReadFile(srcEnv)
	if err != nil {
		t.Fatalf("read %s: %v", srcEnv, err)
	}
	envYaml := filepath.Join(envsDir, "e2e-minimal.yaml")
	if err := os.WriteFile(envYaml, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", envYaml, err)
	}

	ns := "e2e-" + randHex(4)
	port := 19900 + int(randByte())
	t.Logf("ORBIT_NAMESPACE=%s ORBIT_DASHBOARD_PORT=%d", ns, port)

	env := &e2eEnv{
		home:      home,
		binary:    binary,
		envYaml:   envYaml,
		namespace: ns,
		port:      port,
	}
	env.run(t, "env", "use", envYaml)

	t.Cleanup(func() {
		if t.Failed() {
			if logData, err := os.ReadFile(filepath.Join(home, "daemon.log")); err == nil {
				t.Logf("daemon.log:\n%s", logData)
			}
		}
		// Best-effort cleanup. Errors are logged but don't fail the test.
		_, _ = env.runNoFail(t, "down")
		_, _ = env.runNoFail(t, "daemon", "stop")
	})
	return env
}

// findRepoFile walks up from cwd looking for a file path. Lets the test run
// from any package under the repo root.
func findRepoFile(t *testing.T, rel string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	dir := cwd
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate %s from cwd %s", rel, cwd)
	return ""
}

func (e *e2eEnv) cmd(args ...string) *exec.Cmd {
	cmd := exec.Command(e.binary, args...)
	cmd.Env = append(os.Environ(),
		"ORBIT_HOME="+e.home,
		"ORBIT_NAMESPACE="+e.namespace,
		fmt.Sprintf("ORBIT_DASHBOARD_PORT=%d", e.port),
	)
	return cmd
}

func (e *e2eEnv) run(t *testing.T, args ...string) string {
	t.Helper()
	out, err := e.runNoFail(t, args...)
	if err != nil {
		t.Fatalf("orbit %s: %v\noutput:\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (e *e2eEnv) runNoFail(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := e.cmd(args...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	if testing.Verbose() {
		t.Logf("$ orbit %s\n%s", strings.Join(args, " "), out)
	}
	return out, err
}

type e2eStatus struct {
	Daemon struct {
		Running bool   `json:"running"`
		Version string `json:"version,omitempty"`
	} `json:"daemon"`
	Services []struct {
		Name  string `json:"name"`
		Kind  string `json:"kind"`
		State string `json:"state"`
	} `json:"services"`
}

type e2eCLIEnvelope struct {
	SchemaVersion      string           `json:"schema_version"`
	OK                 bool             `json:"ok"`
	Command            string           `json:"command"`
	Data               json.RawMessage  `json:"data"`
	RecommendedActions []cli.JSONAction `json:"recommended_actions"`
	Error              *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func parseE2EEnvelope(t *testing.T, out string) e2eCLIEnvelope {
	t.Helper()
	var env e2eCLIEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parse cli envelope: %v\n%s", err, out)
	}
	if env.SchemaVersion != "orbit.cli.v1" {
		t.Fatalf("schema_version = %q", env.SchemaVersion)
	}
	return env
}

func (e *e2eEnv) status(t *testing.T) e2eStatus {
	t.Helper()
	out := e.run(t, "status", "--json")
	envelope := parseE2EEnvelope(t, out)
	if !envelope.OK {
		t.Fatalf("status envelope not ok: %+v\n%s", envelope.Error, out)
	}
	var s e2eStatus
	if err := json.Unmarshal(envelope.Data, &s); err != nil {
		t.Fatalf("parse status data: %v\n%s", err, out)
	}
	return s
}

func (e *e2eEnv) serviceState(t *testing.T, name string) string {
	t.Helper()
	for _, svc := range e.status(t).Services {
		if svc.Name == name {
			return svc.State
		}
	}
	return "(missing)"
}

func (e *e2eEnv) waitFor(t *testing.T, desc string, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", desc)
}

// Covers: daemon start does NOT auto-launch containers.
func TestE2E_DaemonStartAlone(t *testing.T) {
	env := setupE2E(t)
	env.run(t, "daemon", "start")

	s := env.status(t)
	if !s.Daemon.Running {
		t.Fatal("expected daemon running, got not running")
	}
	for _, svc := range s.Services {
		if svc.State != "stopped" {
			t.Errorf("%s state = %s, want stopped (daemon start should not auto-launch)", svc.Name, svc.State)
		}
	}
}

// Covers: orbit up --infra starts containers to healthy.
func TestE2E_UpInfra(t *testing.T) {
	env := setupE2E(t)
	env.run(t, "daemon", "start")
	_, _ = env.runNoFail(t, "up", "--infra")

	env.waitFor(t, "redis healthy", e2eReadyTimeout, func() bool {
		return env.serviceState(t, "redis") == "healthy"
	})
}

// Covers: daemon stop keeps containers running; daemon start re-adopts them.
// This is the C-refactor's headline scenario.
func TestE2E_RestartAdoptsContainers(t *testing.T) {
	env := setupE2E(t)
	env.run(t, "daemon", "start")
	_, _ = env.runNoFail(t, "up", "--infra")
	env.waitFor(t, "redis healthy", e2eReadyTimeout, func() bool {
		return env.serviceState(t, "redis") == "healthy"
	})

	env.run(t, "daemon", "stop")

	// Docker containers must still be running after daemon stop.
	redisName := "orbit-" + env.namespace + "-redis"
	if !containerRunning(t, redisName) {
		t.Fatalf("%s container died when daemon stopped", redisName)
	}

	env.run(t, "daemon", "start")

	env.waitFor(t, "redis re-adopted as healthy", e2eBootTimeout, func() bool {
		return env.serviceState(t, "redis") == "healthy"
	})
}

// Covers: orbit down stops containers but leaves daemon running.
func TestE2E_DownStopsContainersKeepsDaemon(t *testing.T) {
	env := setupE2E(t)
	env.run(t, "daemon", "start")
	_, _ = env.runNoFail(t, "up", "--infra")
	env.waitFor(t, "redis healthy", e2eReadyTimeout, func() bool {
		return env.serviceState(t, "redis") == "healthy"
	})

	env.run(t, "down")

	s := env.status(t)
	if !s.Daemon.Running {
		t.Error("daemon should still be running after orbit down")
	}
	if state := env.serviceState(t, "redis"); state != "stopped" {
		t.Errorf("redis state = %s after down, want stopped", state)
	}
}

func TestE2E_SwitchStopsPreviousEnvironmentBeforeSuccess(t *testing.T) {
	env := setupE2E(t)
	env.run(t, "daemon", "start")
	_, _ = env.runNoFail(t, "up", "--infra")
	env.waitFor(t, "redis healthy", e2eReadyTimeout, func() bool {
		return env.serviceState(t, "redis") == "healthy"
	})

	emptyEnv := filepath.Join(env.home, "envs", "empty.yaml")
	if err := os.WriteFile(emptyEnv, []byte("version: \"2\"\ncontainers: {}\nservices: {}\n"), 0o644); err != nil {
		t.Fatalf("write empty env: %v", err)
	}
	env.run(t, "switch", emptyEnv)

	redisName := "orbit-" + env.namespace + "-redis"
	if containerRunning(t, redisName) {
		t.Fatalf("%s still running after switch reported success", redisName)
	}
	if services := env.status(t).Services; len(services) != 0 {
		t.Fatalf("new environment services = %+v, want none", services)
	}
}

// Covers: `orbit env sync --url file://<local-repo>` clones a local git repo
// and copies its envs/ tree into ORBIT_HOME/envs/.
func TestE2E_EnvSync(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	binary := findOrbitBinary(t)

	home, err := os.MkdirTemp("/tmp", "orb-")
	if err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })

	// Build a local git repo containing envs/example.yaml.
	repo, err := os.MkdirTemp("/tmp", "orb-envrepo-")
	if err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo) })

	srcEnv := findRepoFile(t, filepath.Join("envs", "example.yaml"))
	data, err := os.ReadFile(srcEnv)
	if err != nil {
		t.Fatalf("read example.yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "envs"), 0o755); err != nil {
		t.Fatalf("mkdir repo/envs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "envs", "example.yaml"), data, 0o644); err != nil {
		t.Fatalf("write example.yaml: %v", err)
	}
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		// Avoid depending on host git user config.
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@example.com",
			"GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	runGit("init", "-q", "-b", "main")
	runGit("add", ".")
	runGit("commit", "-q", "-m", "init")

	// Run `orbit env sync --url file://<repo>` with isolated ORBIT_HOME.
	cmd := exec.Command(binary, "env", "sync", "--url", "file://"+repo)
	cmd.Env = append(os.Environ(),
		"ORBIT_HOME="+home,
		"ORBIT_NAMESPACE=e2e-"+randHex(4),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("orbit env sync: %v\n%s", err, out)
	}
	if testing.Verbose() {
		t.Logf("$ orbit env sync --url file://%s\n%s", repo, out)
	}

	dest := filepath.Join(home, "envs", "example.yaml")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("expected %s to exist after sync, stat err=%v\noutput:\n%s", dest, err, out)
	}
}

// Covers: `orbit up` preflight blocks start-up when ~/.orbit/envs is missing.
func TestE2E_UpBlockedWhenNoEnvs(t *testing.T) {
	binary := findOrbitBinary(t)

	home, err := os.MkdirTemp("/tmp", "orb-")
	if err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })

	ns := "e2e-" + randHex(4)
	port := 19900 + int(randByte())

	mkCmd := func(args ...string) *exec.Cmd {
		c := exec.Command(binary, args...)
		c.Env = append(os.Environ(),
			"ORBIT_HOME="+home,
			"ORBIT_NAMESPACE="+ns,
			fmt.Sprintf("ORBIT_DASHBOARD_PORT=%d", port),
		)
		return c
	}
	t.Cleanup(func() {
		_ = mkCmd("down").Run()
		_ = mkCmd("daemon", "stop").Run()
	})

	// `orbit up` runs preflight before touching the daemon; it must abort
	// cleanly when envs are missing, without starting containers.
	out, err := mkCmd("up").CombinedOutput()
	if err == nil {
		t.Fatalf("expected `orbit up` to fail with empty envs, got success\noutput:\n%s", out)
	}
	combined := string(out)
	if !strings.Contains(combined, "not ready") && !strings.Contains(combined, "orbit init") {
		t.Errorf("expected output to mention `not ready` or `orbit init`, got:\n%s", combined)
	}
}

func TestE2E_AgentJSONWorkflow(t *testing.T) {
	env := setupE2E(t)

	upOut := env.run(t, "up", "--infra", "--json")
	upEnvelope := parseE2EEnvelope(t, upOut)
	if !upEnvelope.OK {
		t.Fatalf("up envelope not ok: %+v\n%s", upEnvelope.Error, upOut)
	}

	statusOut := env.run(t, "status", "--json")
	statusEnvelope := parseE2EEnvelope(t, statusOut)
	if !statusEnvelope.OK {
		t.Fatalf("status envelope not ok: %+v\n%s", statusEnvelope.Error, statusOut)
	}
	var status e2eStatus
	if err := json.Unmarshal(statusEnvelope.Data, &status); err != nil {
		t.Fatalf("status data: %v\n%s", err, statusOut)
	}
	if !status.Daemon.Running {
		t.Fatal("daemon should be running after up --infra --json")
	}

	env.waitFor(t, "redis healthy", e2eReadyTimeout, func() bool {
		return env.serviceState(t, "redis") == "healthy"
	})

	logsOut := env.run(t, "logs", "redis", "--json")
	logsEnvelope := parseE2EEnvelope(t, logsOut)
	if !logsEnvelope.OK {
		t.Fatalf("logs envelope not ok: %+v\n%s", logsEnvelope.Error, logsOut)
	}
	var logsData struct {
		Service string   `json:"service"`
		Lines   []string `json:"lines"`
	}
	if err := json.Unmarshal(logsEnvelope.Data, &logsData); err != nil {
		t.Fatalf("logs data: %v\n%s", err, logsEnvelope.Data)
	}
	if logsData.Service != "redis" {
		t.Fatalf("logs service = %q", logsData.Service)
	}
}

func TestE2E_PortConflictReportsOwnerAndSafeInspection(t *testing.T) {
	binary := findOrbitBinary(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	servicePort := listener.Addr().(*net.TCPAddr).Port

	dashboardListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dashboardPort := dashboardListener.Addr().(*net.TCPAddr).Port
	_ = dashboardListener.Close()

	home, err := os.MkdirTemp("/tmp", "orb-port-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "port-conflict.yaml")
	configYAML := fmt.Sprintf(`version: "2"
services:
  occupied-service:
    type: python
    path: %q
    command: python3 -m http.server %d
    ports:
      http: %d
    health_check:
      type: http
      path: /
      port: %d
`, workspace, servicePort, servicePort, servicePort)
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	command := func(args ...string) *exec.Cmd {
		fullArgs := append([]string{"-c", configPath}, args...)
		cmd := exec.Command(binary, fullArgs...)
		cmd.Env = append(os.Environ(),
			"ORBIT_HOME="+home,
			"ORBIT_NAMESPACE=e2e-port-conflict",
			fmt.Sprintf("ORBIT_DASHBOARD_PORT=%d", dashboardPort),
		)
		return cmd
	}
	t.Cleanup(func() { _ = command("daemon", "stop").Run() })

	out, err := command("up", "--json").Output()
	if err == nil {
		t.Fatalf("up unexpectedly accepted occupied port %d", servicePort)
	}
	envelope := parseE2EEnvelope(t, string(out))
	if envelope.OK || envelope.Error == nil {
		t.Fatalf("expected error envelope: %s", out)
	}
	for _, evidence := range []string{
		"service_start_failed",
		strconv.Itoa(servicePort),
		"occupied-service",
		strconv.Itoa(os.Getpid()),
		"inspect it with",
	} {
		if !strings.Contains(envelope.Error.Code+" "+envelope.Error.Message, evidence) {
			t.Errorf("error envelope missing %q:\n%s", evidence, out)
		}
	}
	if strings.Contains(strings.ToLower(envelope.Error.Message), "kill") {
		t.Errorf("error should not suggest killing a process:\n%s", out)
	}
}

func TestE2E_StatusExplainsRootFailureAndBlockedDependent(t *testing.T) {
	binary := findOrbitBinary(t)
	home, err := os.MkdirTemp("/tmp", "orb-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	workspace := t.TempDir()
	envsDir := filepath.Join(home, "envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(envsDir, "recovery.yaml")
	configYAML := fmt.Sprintf(`version: "2"
services:
  api-runtime:
    type: shell
    path: %q
    command: orbit-e2e-missing-executable serve
  web-app:
    type: shell
    path: %q
    command: python3 -m http.server 18765
    depends_on: [api-runtime]
`, workspace, workspace)
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	dashboardListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dashboardPort := dashboardListener.Addr().(*net.TCPAddr).Port
	_ = dashboardListener.Close()
	namespace := "e2e-recovery-" + randHex(4)

	command := func(args ...string) *exec.Cmd {
		fullArgs := append([]string{"-c", configPath}, args...)
		cmd := exec.Command(binary, fullArgs...)
		cmd.Env = append(os.Environ(),
			"ORBIT_HOME="+home,
			"ORBIT_NAMESPACE="+namespace,
			fmt.Sprintf("ORBIT_DASHBOARD_PORT=%d", dashboardPort),
		)
		return cmd
	}
	t.Cleanup(func() { _ = command("daemon", "stop").Run() })

	if output, err := command("up").CombinedOutput(); err == nil {
		t.Fatalf("up unexpectedly succeeded:\n%s", output)
	}

	human, err := command("status").CombinedOutput()
	if err != nil {
		t.Fatalf("human status: %v\n%s", err, human)
	}
	for _, evidence := range []string{
		`exec: "orbit-e2e-missing-executable": executable file not found`,
		"blocked by api-runtime",
		"orbit logs api-runtime",
		"orbit restart api-runtime",
	} {
		if !bytes.Contains(human, []byte(evidence)) {
			t.Fatalf("human status missing %q:\n%s", evidence, human)
		}
	}

	jsonOutput, err := command("status", "--json").Output()
	if err != nil {
		t.Fatalf("json status: %v", err)
	}
	envelope := parseE2EEnvelope(t, string(jsonOutput))
	if !envelope.OK {
		t.Fatalf("status envelope not ok: %s", jsonOutput)
	}
	var status struct {
		Services []jsonService `json:"services"`
	}
	if err := json.Unmarshal(envelope.Data, &status); err != nil {
		t.Fatalf("status data: %v\n%s", err, jsonOutput)
	}
	services := make(map[string]jsonService, len(status.Services))
	for _, service := range status.Services {
		services[service.Name] = service
	}
	if services["api-runtime"].StateReason == "" {
		t.Fatalf("api-runtime state_reason empty:\n%s", jsonOutput)
	}
	if services["web-app"].BlockedBy != "api-runtime" {
		t.Fatalf("web-app blocked_by = %q:\n%s", services["web-app"].BlockedBy, jsonOutput)
	}
	commands := make(map[string]bool, len(envelope.RecommendedActions))
	for _, action := range envelope.RecommendedActions {
		commands[action.Command] = true
	}
	for _, wanted := range []string{
		"orbit logs api-runtime --json",
		"orbit restart api-runtime --json",
	} {
		if !commands[wanted] {
			t.Fatalf("recommended_actions missing %q: %+v", wanted, envelope.RecommendedActions)
		}
	}
	if commands["orbit logs web-app --json"] {
		t.Fatalf("recommended_actions points to dependent without logs: %+v", envelope.RecommendedActions)
	}
}

func TestE2E_InitDoesNotClaimSuccessWhenEnvSyncFails(t *testing.T) {
	binary := findOrbitBinary(t)
	home, err := os.MkdirTemp("/tmp", "orb-init-failure-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	workspace := t.TempDir()
	missingRepo := "file://" + filepath.Join(t.TempDir(), "missing-repo")

	command := exec.Command(binary, "init", "--yes", "--env-repo", missingRepo)
	command.Dir = workspace
	command.Env = append(os.Environ(), "ORBIT_HOME="+home)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("incomplete init must return non-zero:\n%s", output)
	}
	for _, wanted := range []string{
		"Setup is incomplete",
		"Verify the repository URL and Git access",
		"Next: orbit init",
	} {
		if !bytes.Contains(output, []byte(wanted)) {
			t.Fatalf("init output missing %q:\n%s", wanted, output)
		}
	}
	if bytes.Contains(output, []byte("Setup complete!")) {
		t.Fatalf("init falsely claims success:\n%s", output)
	}
}

func TestE2E_InitJSONFailureRetainsDiagnosticData(t *testing.T) {
	binary := findOrbitBinary(t)
	home := t.TempDir()
	workspace := t.TempDir()
	missingRepo := "file://" + filepath.Join(t.TempDir(), "missing-repo")

	command := exec.Command(binary, "init", "--yes", "--env-repo", missingRepo, "--json")
	command.Dir = workspace
	command.Env = append(os.Environ(), "ORBIT_HOME="+home)
	output, err := command.Output()
	if err == nil {
		t.Fatalf("incomplete JSON init must return non-zero:\n%s", output)
	}
	envelope := parseE2EEnvelope(t, string(output))
	if envelope.OK || envelope.Error == nil || envelope.Error.Code != "env_repo_access" {
		t.Fatalf("envelope = %+v:\n%s", envelope, output)
	}
	var data struct {
		Ready bool `json:"ready"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("decode diagnostic data: %v\n%s", err, envelope.Data)
	}
	if data.Ready {
		t.Fatalf("diagnostic data = %s", envelope.Data)
	}
	commands := make(map[string]bool, len(envelope.RecommendedActions))
	for _, action := range envelope.RecommendedActions {
		commands[action.Command] = true
	}
	if !commands["orbit init --yes --json"] {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
}

func TestE2E_EnvSyncClassifiesRepositoryAccessFailure(t *testing.T) {
	binary := findOrbitBinary(t)
	home := t.TempDir()
	missingRepo := "file://" + filepath.Join(t.TempDir(), "missing-repo")
	command := exec.Command(binary, "env", "sync", "--url", missingRepo, "--json")
	command.Env = append(os.Environ(), "ORBIT_HOME="+home)
	output, err := command.Output()
	if err == nil {
		t.Fatalf("env sync unexpectedly succeeded:\n%s", output)
	}
	envelope := parseE2EEnvelope(t, string(output))
	if envelope.Error == nil || envelope.Error.Code != "env_repo_access" {
		t.Fatalf("error = %+v:\n%s", envelope.Error, output)
	}
	if len(envelope.RecommendedActions) != 1 || envelope.RecommendedActions[0].Command != "orbit env sync --json" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
}

// findOrbitBinary mirrors setupE2E's binary discovery without the docker skip
// and without creating an e2eEnv. Useful for tests that don't need a full env.
func findOrbitBinary(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("ORBIT_BIN"); binary != "" {
		return binary
	}
	if path, err := exec.LookPath("orbit"); err == nil {
		return path
	}
	t.Fatal("orbit binary not found; set ORBIT_BIN or run `make build`")
	return ""
}

// containerRunning returns true if a docker container with the given name
// (exact match) is in running state.
func containerRunning(t *testing.T, name string) bool {
	t.Helper()
	out, err := exec.Command("docker", "ps", "--filter", "name=^"+name+"$", "--filter", "status=running", "--format", "{{.Names}}").Output()
	if err != nil {
		t.Fatalf("docker ps: %v", err)
	}
	return strings.TrimSpace(string(out)) == name
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randByte() byte {
	b := make([]byte, 1)
	_, _ = rand.Read(b)
	return b[0]
}

// goos reports the runtime OS (used in some conditional test assertions).
func goos() string { return runtime.GOOS }

var (
	_ = fmt.Sprintf
	_ = goos
)
