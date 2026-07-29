//go:build e2e

package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/iml885203/orbit/platform"
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
	Resources []struct {
		Name  string `json:"name"`
		Kind  string `json:"kind"`
		State string `json:"state"`
	} `json:"resources"`
}

type e2eCLIEnvelope struct {
	SchemaVersion      string           `json:"schema_version"`
	OK                 bool             `json:"ok"`
	Command            string           `json:"command"`
	Data               json.RawMessage  `json:"data"`
	RecommendedActions []cli.JSONAction `json:"recommended_actions"`
	Error              *struct {
		Code        string `json:"code"`
		Message     string `json:"message"`
		Hint        string `json:"hint,omitempty"`
		Retryable   bool   `json:"retryable"`
		NextCommand string `json:"next_command,omitempty"`
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

func reserveLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func readPIDFile(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse pid file: %v", err)
	}
	return pid
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
	for _, svc := range e.status(t).Resources {
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
	for _, svc := range s.Resources {
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

func TestE2E_DaemonRestartThenUpRecoversTheWholeEnvironment(t *testing.T) {
	binary := findOrbitBinary(t)
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	servicePort := reserveLocalPort(t)
	redisPort := reserveLocalPort(t)
	dashboardPort := reserveLocalPort(t)

	home, err := os.MkdirTemp("/tmp", "orb-restart-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	workspace := t.TempDir()
	pidPath := filepath.Join(workspace, "api.pid")
	appPath := filepath.Join(workspace, "app.py")
	appSource := fmt.Sprintf(`import http.server
import os

with open(%q, "w", encoding="utf-8") as pid_file:
    pid_file.write(str(os.getpid()))

server = http.server.ThreadingHTTPServer(("127.0.0.1", %d), http.server.SimpleHTTPRequestHandler)
server.serve_forever()
`, pidPath, servicePort)
	if err := os.WriteFile(appPath, []byte(appSource), 0o600); err != nil {
		t.Fatal(err)
	}

	envsDir := filepath.Join(home, "envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(envsDir, "restart-recovery.yaml")
	configYAML := fmt.Sprintf(`version: "2"
settings:
  health_check_interval: 1s
  docker_poll_interval: 1s
containers:
  redis:
    image: redis:7.4-alpine
    ports:
      redis: "%d:6379"
    health_check:
      type: tcp
      port: %d
services:
  api:
    type: python
    path: %q
    command: python3 app.py
    ports:
      http: %d
    depends_on: [redis]
    health_check:
      type: http
      path: /
      port: %d
`, redisPort, redisPort, workspace, servicePort, servicePort)
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	namespace := "e2e-restart-" + randHex(4)
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
	t.Cleanup(func() {
		_ = command("down").Run()
		_ = command("daemon", "stop").Run()
	})

	initialUp, err := command("up", "--json").Output()
	if err != nil {
		t.Fatalf("initial up: %v\n%s", err, initialUp)
	}
	if envelope := parseE2EEnvelope(t, string(initialUp)); !envelope.OK {
		t.Fatalf("initial up envelope = %+v\n%s", envelope, initialUp)
	}

	containerName := "orbit-" + namespace + "-redis"
	containerIDBefore, err := exec.Command("docker", "inspect", "--format", "{{.Id}}", containerName).Output()
	if err != nil {
		t.Fatalf("inspect initial redis: %v", err)
	}
	apiPIDBefore := readPIDFile(t, pidPath)

	restartOutput, err := command("daemon", "restart", "--json").Output()
	if err != nil {
		t.Fatalf("daemon restart: %v\n%s", err, restartOutput)
	}
	restartEnvelope := parseE2EEnvelope(t, string(restartOutput))
	if !restartEnvelope.OK {
		t.Fatalf("daemon restart envelope = %+v\n%s", restartEnvelope, restartOutput)
	}
	var restartData struct {
		RequestedServiceShutdown bool `json:"requested_service_shutdown"`
	}
	if err := json.Unmarshal(restartEnvelope.Data, &restartData); err != nil {
		t.Fatalf("parse daemon restart data: %v\n%s", err, restartOutput)
	}
	if !restartData.RequestedServiceShutdown {
		t.Fatalf("daemon restart did not request service shutdown:\n%s", restartOutput)
	}

	secondUp, err := command("up", "--json").Output()
	if err != nil {
		t.Fatalf("up after daemon restart: %v\n%s", err, secondUp)
	}
	secondEnvelope := parseE2EEnvelope(t, string(secondUp))
	if !secondEnvelope.OK {
		t.Fatalf("second up envelope = %+v\n%s", secondEnvelope, secondUp)
	}
	var upData struct {
		DegradedResources []string `json:"degraded_resources"`
		TimedOutResources []string `json:"timed_out_resources"`
	}
	if err := json.Unmarshal(secondEnvelope.Data, &upData); err != nil {
		t.Fatalf("parse second up data: %v\n%s", err, secondUp)
	}
	if len(upData.DegradedResources) > 0 || len(upData.TimedOutResources) > 0 {
		t.Fatalf("restart recovery left unhealthy resources: %+v\n%s", upData, secondUp)
	}

	containerIDAfter, err := exec.Command("docker", "inspect", "--format", "{{.Id}}", containerName).Output()
	if err != nil {
		t.Fatalf("inspect adopted redis: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(containerIDAfter), bytes.TrimSpace(containerIDBefore)) {
		t.Fatalf("daemon restart recreated redis: before=%s after=%s", containerIDBefore, containerIDAfter)
	}
	apiPIDAfter := readPIDFile(t, pidPath)
	if apiPIDAfter == apiPIDBefore {
		t.Fatalf("host service kept stale pid %d across daemon restart", apiPIDAfter)
	}
	if platform.IsProcessAlive(apiPIDBefore) {
		t.Fatalf("old host service pid %d survived daemon restart", apiPIDBefore)
	}
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
	if resources := env.status(t).Resources; len(resources) != 0 {
		t.Fatalf("new environment resources = %+v, want none", resources)
	}
}

func TestE2E_SwitchRejectsInvalidTargetBeforeStoppingCurrentEnvironment(t *testing.T) {
	env := setupE2E(t)
	env.run(t, "daemon", "start")
	upOutput := env.run(t, "up", "redis")
	for _, want := range []string{"Starting redis.", "redis is healthy."} {
		if !strings.Contains(upOutput, want) {
			t.Fatalf("resource-specific up output missing %q:\n%s", want, upOutput)
		}
	}
	if strings.Contains(upOutput, "requested services") {
		t.Fatalf("container up output mislabeled redis as a service:\n%s", upOutput)
	}
	env.waitFor(t, "redis healthy", e2eReadyTimeout, func() bool {
		return env.serviceState(t, "redis") == "healthy"
	})

	invalidEnv := filepath.Join(env.home, "envs", "invalid.yaml")
	if err := os.WriteFile(invalidEnv, []byte("version: \"2\"\nservices:\n  broken: [\n"), 0o644); err != nil {
		t.Fatalf("write invalid env: %v", err)
	}

	output, err := env.runNoFail(t, "switch", invalidEnv)
	if err == nil {
		t.Fatalf("invalid switch succeeded:\n%s", output)
	}
	for _, evidence := range []string{"validate target environment invalid.yaml", "invalid.yaml"} {
		if !strings.Contains(output, evidence) {
			t.Fatalf("switch error missing %q:\n%s", evidence, output)
		}
	}

	jsonOutput, err := env.runNoFail(t, "switch", invalidEnv, "--json")
	if err == nil {
		t.Fatalf("invalid JSON switch succeeded:\n%s", jsonOutput)
	}
	envelope := parseE2EEnvelope(t, jsonOutput)
	if envelope.OK || envelope.Error == nil {
		t.Fatalf("invalid switch envelope = %+v", envelope)
	}
	if envelope.Error.Code != "invalid_environment" || !strings.Contains(envelope.Error.Hint, "Fix the reported environment file") {
		t.Fatalf("JSON switch error = %+v", envelope.Error)
	}
	if !strings.Contains(envelope.Error.Message, "validate target environment invalid.yaml") {
		t.Fatalf("JSON switch error = %+v", envelope.Error)
	}
	expectedRetry := "orbit switch " + invalidEnv + " --json"
	if len(envelope.RecommendedActions) != 1 || envelope.RecommendedActions[0].Command != expectedRetry {
		t.Fatalf("recommended actions = %+v", envelope.RecommendedActions)
	}

	redisName := "orbit-" + env.namespace + "-redis"
	if !containerRunning(t, redisName) {
		t.Fatalf("%s stopped after the target environment was rejected", redisName)
	}
	if state := env.serviceState(t, "redis"); state != "healthy" {
		t.Fatalf("current redis state = %s after rejected switch, want healthy", state)
	}
	if current := strings.TrimSpace(env.run(t, "env", "current")); current != env.envYaml {
		t.Fatalf("current environment = %q after rejected switch, want %q", current, env.envYaml)
	}
}

func TestE2E_EnvApplyRestoresRunningResourcesAndRejectsInvalidChangesSafely(t *testing.T) {
	env := setupE2E(t)
	env.run(t, "daemon", "start")
	env.run(t, "up", "redis")

	original, err := os.ReadFile(env.envYaml)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	updated := bytes.Replace(original, []byte("shutdown_timeout: 10s"), []byte("shutdown_timeout: 11s"), 1)
	if bytes.Equal(updated, original) {
		t.Fatal("test fixture no longer contains the expected shutdown timeout")
	}
	if err := os.WriteFile(env.envYaml, updated, 0o644); err != nil {
		t.Fatalf("update env: %v", err)
	}

	previousPID := readE2EDaemonPID(t, env.home)
	output := env.run(t, "env", "apply", "--json")
	envelope := parseE2EEnvelope(t, output)
	if !envelope.OK {
		t.Fatalf("env apply failed: %+v\n%s", envelope.Error, output)
	}
	var applied environmentApplyJSONData
	if err := json.Unmarshal(envelope.Data, &applied); err != nil {
		t.Fatalf("parse env apply data: %v\n%s", err, output)
	}
	if !applied.Applied || !slices.Equal(applied.PreviouslyRunning, []string{"redis"}) ||
		!slices.Equal(applied.RestoredResources, []string{"redis"}) {
		t.Fatalf("env apply data = %+v", applied)
	}
	if currentPID := readE2EDaemonPID(t, env.home); currentPID == previousPID {
		t.Fatalf("daemon pid remained %d after applying a changed config", currentPID)
	}
	if state := env.serviceState(t, "redis"); state != "healthy" {
		t.Fatalf("redis state = %s after apply, want healthy", state)
	}

	invalid := bytes.Replace(updated, []byte(`version: "2"`), []byte(`version: "999"`), 1)
	if err := os.WriteFile(env.envYaml, invalid, 0o644); err != nil {
		t.Fatalf("write invalid env: %v", err)
	}
	previousPID = readE2EDaemonPID(t, env.home)
	output, err = env.runNoFail(t, "env", "apply", "--json")
	if err == nil {
		t.Fatalf("invalid environment was applied:\n%s", output)
	}
	envelope = parseE2EEnvelope(t, output)
	if envelope.OK || envelope.Error == nil ||
		!strings.Contains(envelope.Error.Message, "cannot apply environment changes") {
		t.Fatalf("invalid apply envelope = %+v", envelope)
	}
	if currentPID := readE2EDaemonPID(t, env.home); currentPID != previousPID {
		t.Fatalf("daemon pid changed from %d to %d after invalid apply", previousPID, currentPID)
	}
	if !containerRunning(t, "orbit-"+env.namespace+"-redis") {
		t.Fatal("redis stopped after invalid environment was rejected")
	}
}

func readE2EDaemonPID(t *testing.T, home string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "orbit.pid"))
	if err != nil {
		t.Fatalf("read daemon pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse daemon pid %q: %v", data, err)
	}
	return pid
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

func TestE2E_UpEmptyEnvironmentCompletesImmediately(t *testing.T) {
	binary := findOrbitBinary(t)
	home, err := os.MkdirTemp("/tmp", "orb-empty-")
	if err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })

	envsDir := filepath.Join(home, "envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatalf("mkdir envs: %v", err)
	}
	envYaml := filepath.Join(envsDir, "empty.yaml")
	if err := os.WriteFile(envYaml, []byte("version: \"2\"\ncontainers: {}\nservices: {}\n"), 0o644); err != nil {
		t.Fatalf("write empty env: %v", err)
	}

	namespace := "e2e-empty-" + randHex(4)
	port := 19900 + int(randByte())
	command := func(ctx context.Context, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = append(os.Environ(),
			"ORBIT_HOME="+home,
			"ORBIT_NAMESPACE="+namespace,
			fmt.Sprintf("ORBIT_DASHBOARD_PORT=%d", port),
		)
		return cmd
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = command(ctx, "daemon", "stop").Run()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if output, err := command(ctx, "env", "use", envYaml).CombinedOutput(); err != nil {
		t.Fatalf("select empty env: %v\n%s", err, output)
	}

	started := time.Now()
	human, err := command(ctx, "up").CombinedOutput()
	if err != nil {
		t.Fatalf("human up failed: %v\n%s", err, human)
	}
	if elapsed := time.Since(started); elapsed >= 3*time.Second {
		t.Fatalf("human up took %s, want an immediate no-op", elapsed)
	}
	if !bytes.Contains(human, []byte("No resources are enabled for this environment.")) {
		t.Fatalf("human output did not explain the no-op:\n%s", human)
	}
	if bytes.Contains(human, []byte("starting 0")) {
		t.Fatalf("human output exposed an implementation count:\n%s", human)
	}

	jsonOutput, err := command(ctx, "up", "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("json up failed: %v\n%s", err, jsonOutput)
	}
	envelope := parseE2EEnvelope(t, string(jsonOutput))
	if !envelope.OK {
		t.Fatalf("json up envelope not ok: %+v\n%s", envelope.Error, jsonOutput)
	}
	var data lifecycleJSONData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("parse lifecycle data: %v\n%s", err, jsonOutput)
	}
	if data.Message != "No resources are enabled for this environment." {
		t.Fatalf("json message = %q", data.Message)
	}
	if len(data.RequestedResources) != 0 || len(data.Resources) != 0 {
		t.Fatalf("json lifecycle data = %+v, want no affected resources", data)
	}
	if len(envelope.RecommendedActions) != 1 || envelope.RecommendedActions[0].Command != "orbit open --json" {
		t.Fatalf("recommended actions = %+v", envelope.RecommendedActions)
	}

	conflictOutput, err := command(ctx, "up", "api", "--infra", "--json").CombinedOutput()
	if err == nil {
		t.Fatalf("conflicting selectors succeeded:\n%s", conflictOutput)
	}
	conflict := parseE2EEnvelope(t, string(conflictOutput))
	if conflict.Error == nil || conflict.Error.Code != "invalid_argument" {
		t.Fatalf("conflicting selector error = %+v", conflict.Error)
	}
	if len(conflict.RecommendedActions) != 0 {
		t.Fatalf("conflicting selector actions = %+v, want none", conflict.RecommendedActions)
	}

	unknownOutput, err := command(ctx, "up", "--group", "typo", "--json").CombinedOutput()
	if err == nil {
		t.Fatalf("unknown group succeeded:\n%s", unknownOutput)
	}
	unknown := parseE2EEnvelope(t, string(unknownOutput))
	if unknown.Error == nil || unknown.Error.Code != "invalid_argument" {
		t.Fatalf("unknown group error = %+v", unknown.Error)
	}
	if !strings.Contains(unknown.Error.Message, "this environment defines no groups") {
		t.Fatalf("unknown group message = %q", unknown.Error.Message)
	}

	unknownResourceCommands := [][]string{
		{"up", "missing", "--json"},
		{"down", "missing", "--json"},
		{"restart", "missing", "--json"},
		{"logs", "missing", "--json"},
		{"logs", "missing", "--follow", "--json"},
	}
	for _, args := range unknownResourceCommands {
		t.Run(strings.Join(args[:len(args)-1], "_"), func(t *testing.T) {
			started := time.Now()
			output, err := command(ctx, args...).CombinedOutput()
			if err == nil {
				t.Fatalf("unknown resource succeeded:\n%s", output)
			}
			if elapsed := time.Since(started); elapsed >= time.Second {
				t.Fatalf("unknown resource took %s, want immediate feedback\n%s", elapsed, output)
			}
			envelope := parseE2EEnvelope(t, string(output))
			if envelope.Error == nil || envelope.Error.Code != "unknown_resource" {
				t.Fatalf("error = %+v\n%s", envelope.Error, output)
			}
			if !strings.Contains(envelope.Error.Message, "unknown resource: missing") {
				t.Fatalf("message = %q", envelope.Error.Message)
			}
			if len(envelope.RecommendedActions) != 1 ||
				envelope.RecommendedActions[0].Command != "orbit status --json" {
				t.Fatalf("recommended actions = %+v", envelope.RecommendedActions)
			}
		})
	}

	humanUnknown, err := command(ctx, "up", "missing").CombinedOutput()
	if err == nil {
		t.Fatalf("human unknown resource succeeded:\n%s", humanUnknown)
	}
	if !bytes.Contains(humanUnknown, []byte("unknown resource: missing")) {
		t.Fatalf("human error = %s", humanUnknown)
	}
	if bytes.Contains(humanUnknown, []byte("doctor")) {
		t.Fatalf("human error misdiagnosed a name typo as environment trouble:\n%s", humanUnknown)
	}
}

func TestE2E_StatusBeforeInitPointsDirectlyToSetup(t *testing.T) {
	binary := findOrbitBinary(t)
	home := t.TempDir()
	command := func(args ...string) *exec.Cmd {
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), "ORBIT_HOME="+home)
		return cmd
	}

	human, err := command("status").CombinedOutput()
	if err != nil {
		t.Fatalf("human status failed: %v\n%s", err, human)
	}
	for _, evidence := range []string{"Orbit is not set up yet.", "Next: orbit init"} {
		if !bytes.Contains(human, []byte(evidence)) {
			t.Fatalf("human status missing %q:\n%s", evidence, human)
		}
	}
	if bytes.Contains(human, []byte("orbit up")) {
		t.Fatalf("human status suggested startup before setup:\n%s", human)
	}

	output, err := command("status", "--json").Output()
	if err != nil {
		t.Fatalf("JSON status failed: %v\n%s", err, output)
	}
	envelope := parseE2EEnvelope(t, string(output))
	if !envelope.OK {
		t.Fatalf("status envelope = %+v:\n%s", envelope, output)
	}
	var data struct {
		SetupRequired bool   `json:"setup_required"`
		SetupMessage  string `json:"setup_message"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("status data: %v\n%s", err, envelope.Data)
	}
	if !data.SetupRequired || data.SetupMessage == "" {
		t.Fatalf("status data = %+v", data)
	}
	if len(envelope.RecommendedActions) != 1 || envelope.RecommendedActions[0].Command != "orbit init --yes --json" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
}

func TestE2E_InspectBeforeInitMatchesStatusSetupGuidance(t *testing.T) {
	binary := findOrbitBinary(t)
	home := t.TempDir()
	command := func(args ...string) *exec.Cmd {
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), "ORBIT_HOME="+home)
		return cmd
	}

	human, err := command("inspect").CombinedOutput()
	if err != nil {
		t.Fatalf("human inspect failed: %v\n%s", err, human)
	}
	for _, evidence := range []string{"Readiness: setup_required", "Next: orbit init"} {
		if !bytes.Contains(human, []byte(evidence)) {
			t.Fatalf("human inspect missing %q:\n%s", evidence, human)
		}
	}
	if bytes.Contains(human, []byte("quickstart.yaml")) || bytes.Contains(human, []byte("orbit doctor")) {
		t.Fatalf("human inspect invented a selected config or irrelevant diagnosis:\n%s", human)
	}

	output, err := command("inspect", "--json").Output()
	if err != nil {
		t.Fatalf("JSON inspect failed: %v\n%s", err, output)
	}
	envelope := parseE2EEnvelope(t, string(output))
	if !envelope.OK {
		t.Fatalf("inspect envelope = %+v:\n%s", envelope, output)
	}
	var data struct {
		Readiness inspectReadiness  `json:"readiness"`
		Env       inspectEnvSummary `json:"env"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("inspect data: %v\n%s", err, envelope.Data)
	}
	if data.Readiness.State != inspectReadinessSetupRequired || !data.Readiness.Blocked {
		t.Fatalf("readiness = %+v", data.Readiness)
	}
	if data.Env.SelectedName != "" || data.Env.SelectedPath != "" {
		t.Fatalf("env = %+v", data.Env)
	}
	if len(envelope.RecommendedActions) != 1 || envelope.RecommendedActions[0].Command != "orbit init --yes --json" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
}

func TestE2E_InspectConfiguredEnvironmentBeforeUpPointsOnlyToUp(t *testing.T) {
	binary := findOrbitBinary(t)
	home := t.TempDir()
	workspace := t.TempDir()
	envsDir := filepath.Join(home, "envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(envsDir, "ready-to-start.yaml")
	raw := fmt.Sprintf(`version: "2"
services:
  demo-api:
    type: shell
    path: %q
    command: python3 app.py
`, workspace)
	if err := os.WriteFile(envPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	command := func(args ...string) *exec.Cmd {
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), "ORBIT_HOME="+home)
		return cmd
	}
	if output, err := command("env", "use", envPath).CombinedOutput(); err != nil {
		t.Fatalf("select env: %v\n%s", err, output)
	}

	human, err := command("inspect").CombinedOutput()
	if err != nil {
		t.Fatalf("human inspect failed: %v\n%s", err, human)
	}
	for _, evidence := range []string{"Readiness: stopped", "Environment: not running", "Next: orbit up"} {
		if !bytes.Contains(human, []byte(evidence)) {
			t.Fatalf("human inspect missing %q:\n%s", evidence, human)
		}
	}
	if bytes.Contains(human, []byte("daemon start")) || bytes.Contains(human, []byte("orbit doctor")) {
		t.Fatalf("human inspect exposed internal or unrelated recovery:\n%s", human)
	}

	output, err := command("inspect", "--json").Output()
	if err != nil {
		t.Fatalf("JSON inspect failed: %v\n%s", err, output)
	}
	envelope := parseE2EEnvelope(t, string(output))
	var data inspectJSONData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("inspect data: %v\n%s", err, envelope.Data)
	}
	if data.Readiness.State != inspectReadinessStopped || !data.Readiness.Blocked {
		t.Fatalf("readiness = %+v", data.Readiness)
	}
	if data.Resources.Total != 1 || len(data.Resources.Stopped) != 1 || data.Resources.Stopped[0] != "demo-api" {
		t.Fatalf("resources = %+v", data.Resources)
	}
	if len(envelope.RecommendedActions) != 1 || envelope.RecommendedActions[0].Command != "orbit up --json" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
}

func TestE2E_StatusRejectsInvalidSelectedEnvironment(t *testing.T) {
	binary := findOrbitBinary(t)
	home := t.TempDir()
	invalidEnv := filepath.Join(home, "broken.yaml")
	if err := os.WriteFile(invalidEnv, []byte("version: \"2\"\nservices: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := func(args ...string) *exec.Cmd {
		fullArgs := append([]string{"-c", invalidEnv}, args...)
		cmd := exec.Command(binary, fullArgs...)
		cmd.Env = append(os.Environ(), "ORBIT_HOME="+home)
		return cmd
	}

	human, err := command("status").CombinedOutput()
	if err == nil {
		t.Fatalf("human status accepted invalid environment:\n%s", human)
	}
	if !bytes.Contains(human, []byte("active environment broken.yaml is invalid")) {
		t.Fatalf("human status error:\n%s", human)
	}

	output, err := command("status", "--json").Output()
	if err == nil {
		t.Fatalf("JSON status accepted invalid environment:\n%s", output)
	}
	envelope := parseE2EEnvelope(t, string(output))
	if envelope.Error == nil || envelope.Error.Code != "invalid_environment" {
		t.Fatalf("status envelope = %+v:\n%s", envelope, output)
	}
	if len(envelope.RecommendedActions) != 1 || envelope.RecommendedActions[0].Command != envelope.Command {
		t.Fatalf("recommended_actions = %+v, command = %q", envelope.RecommendedActions, envelope.Command)
	}
}

func TestE2E_AgentJSONWorkflow(t *testing.T) {
	env := setupE2E(t)

	upOut := env.run(t, "up", "--infra", "--json")
	upEnvelope := parseE2EEnvelope(t, upOut)
	if !upEnvelope.OK {
		t.Fatalf("up envelope not ok: %+v\n%s", upEnvelope.Error, upOut)
	}
	if len(upEnvelope.RecommendedActions) != 1 || upEnvelope.RecommendedActions[0].Command != "orbit open --json" {
		t.Fatalf("up recommended_actions = %+v", upEnvelope.RecommendedActions)
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
		Resource string   `json:"resource"`
		Lines    []string `json:"lines"`
	}
	if err := json.Unmarshal(logsEnvelope.Data, &logsData); err != nil {
		t.Fatalf("logs data: %v\n%s", err, logsEnvelope.Data)
	}
	if logsData.Resource != "redis" {
		t.Fatalf("logs resource = %q", logsData.Resource)
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
	t.Cleanup(func() {
		_ = command("down").Run()
		_ = command("daemon", "stop").Run()
	})

	doctorOut, err := command("doctor", "--json").Output()
	if err == nil {
		t.Fatalf("doctor unexpectedly accepted occupied port %d:\n%s", servicePort, doctorOut)
	}
	doctorEnvelope := parseE2EEnvelope(t, string(doctorOut))
	if doctorEnvelope.Error == nil || doctorEnvelope.Error.Code != "checks_failed" ||
		len(doctorEnvelope.RecommendedActions) != 1 ||
		strings.HasPrefix(doctorEnvelope.RecommendedActions[0].Command, "orbit up") {
		t.Fatalf("doctor conflict recovery = %+v\n%s", doctorEnvelope, doctorOut)
	}

	out, err := command("up", "--json").Output()
	if err == nil {
		t.Fatalf("up unexpectedly accepted occupied port %d", servicePort)
	}
	envelope := parseE2EEnvelope(t, string(out))
	if envelope.OK || envelope.Error == nil {
		t.Fatalf("expected error envelope: %s", out)
	}
	for _, evidence := range []string{
		"resource_port_conflict",
		strconv.Itoa(servicePort),
		"occupied-service",
	} {
		if !strings.Contains(envelope.Error.Code+" "+envelope.Error.Message, evidence) {
			t.Errorf("error envelope missing %q:\n%s", evidence, out)
		}
	}
	if envelope.Error.NextCommand == "" ||
		len(envelope.RecommendedActions) != 1 ||
		envelope.RecommendedActions[0].Command != envelope.Error.NextCommand {
		t.Errorf("port recovery is not one exact inspection command:\n%s", out)
	}
	if strings.Contains(string(out), "orbit logs") || strings.Contains(string(out), "orbit restart") {
		t.Errorf("port conflict should not recommend logs or a blind restart:\n%s", out)
	}
	if strings.Contains(strings.ToLower(envelope.Error.Message), "kill") {
		t.Errorf("error should not suggest killing a process:\n%s", out)
	}

	emptyLogs, err := command("logs", "occupied-service", "--json").Output()
	if err == nil {
		t.Fatalf("empty logs unexpectedly reported success:\n%s", emptyLogs)
	}
	emptyLogsEnvelope := parseE2EEnvelope(t, string(emptyLogs))
	if emptyLogsEnvelope.Error == nil || emptyLogsEnvelope.Error.Code != "logs_unavailable" ||
		len(emptyLogsEnvelope.RecommendedActions) != 1 ||
		strings.HasPrefix(emptyLogsEnvelope.RecommendedActions[0].Command, "orbit up") {
		t.Fatalf("occupied empty-log recovery = %+v\n%s", emptyLogsEnvelope, emptyLogs)
	}

	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	recoveredDoctorOut, err := command("doctor", "--json").Output()
	if err != nil {
		t.Fatalf("doctor kept stale conflict after release: %v\n%s", err, recoveredDoctorOut)
	}
	recoveredDoctor := parseE2EEnvelope(t, string(recoveredDoctorOut))
	if !recoveredDoctor.OK || len(recoveredDoctor.RecommendedActions) != 1 ||
		recoveredDoctor.RecommendedActions[0].Command != "orbit up occupied-service --json" {
		t.Fatalf("released doctor recovery = %+v\n%s", recoveredDoctor, recoveredDoctorOut)
	}

	releasedLogs, err := command("logs", "occupied-service", "--json").Output()
	if err == nil {
		t.Fatalf("empty logs unexpectedly reported success after release:\n%s", releasedLogs)
	}
	releasedLogsEnvelope := parseE2EEnvelope(t, string(releasedLogs))
	if releasedLogsEnvelope.Error == nil || releasedLogsEnvelope.Error.Code != "logs_unavailable" ||
		len(releasedLogsEnvelope.RecommendedActions) != 1 ||
		releasedLogsEnvelope.RecommendedActions[0].Command != "orbit up occupied-service --json" {
		t.Fatalf("released empty-log recovery = %+v\n%s", releasedLogsEnvelope, releasedLogs)
	}

	retryOut, err := command("up", "occupied-service", "--json").Output()
	if err != nil {
		t.Fatalf("targeted retry failed: %v\n%s", err, retryOut)
	}
	retryEnvelope := parseE2EEnvelope(t, string(retryOut))
	if !retryEnvelope.OK {
		t.Fatalf("targeted retry envelope = %+v\n%s", retryEnvelope, retryOut)
	}
}

func TestE2E_CrashedServiceRecoveryIsLinearAndPreservesHealthyDependency(t *testing.T) {
	binary := findOrbitBinary(t)
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	serviceListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	servicePort := serviceListener.Addr().(*net.TCPAddr).Port
	_ = serviceListener.Close()
	redisListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	redisPort := redisListener.Addr().(*net.TCPAddr).Port
	_ = redisListener.Close()
	dashboardListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dashboardPort := dashboardListener.Addr().(*net.TCPAddr).Port
	_ = dashboardListener.Close()

	home, err := os.MkdirTemp("/tmp", "orb-crash-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	workspace := t.TempDir()
	pidPath := filepath.Join(workspace, "api.pid")
	appPath := filepath.Join(workspace, "app.py")
	appSource := fmt.Sprintf(`import http.server
import os

with open(%q, "w", encoding="utf-8") as pid_file:
    pid_file.write(str(os.getpid()))

server = http.server.ThreadingHTTPServer(("127.0.0.1", %d), http.server.SimpleHTTPRequestHandler)
print("api ready", flush=True)
server.serve_forever()
`, pidPath, servicePort)
	if err := os.WriteFile(appPath, []byte(appSource), 0o600); err != nil {
		t.Fatal(err)
	}

	envsDir := filepath.Join(home, "envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(envsDir, "crash-recovery.yaml")
	configYAML := fmt.Sprintf(`version: "2"
settings:
  health_check_interval: 1s
containers:
  redis:
    image: redis:7.4-alpine
    ports:
      redis: "%d:6379"
    health_check:
      type: tcp
      port: %d
services:
  api:
    type: python
    path: %q
    command: python3 app.py
    ports:
      http: %d
    depends_on: [redis]
    health_check:
      type: http
      path: /
      port: %d
`, redisPort, redisPort, workspace, servicePort, servicePort)
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	namespace := "e2e-crash-" + randHex(4)
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
	t.Cleanup(func() {
		_ = command("down").Run()
		_ = command("daemon", "stop").Run()
	})

	if output, err := command("up", "--json").Output(); err != nil {
		t.Fatalf("initial up: %v\n%s", err, output)
	}
	containerName := "orbit-" + namespace + "-redis"
	containerEvidence := func() string {
		t.Helper()
		output, err := exec.Command(
			"docker", "inspect", "--format",
			"{{.Id}}|{{.State.StartedAt}}|{{.RestartCount}}",
			containerName,
		).Output()
		if err != nil {
			t.Fatalf("inspect %s: %v", containerName, err)
		}
		return strings.TrimSpace(string(output))
	}
	redisBefore := containerEvidence()

	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read api pid: %v", err)
	}
	apiPID, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatalf("parse api pid: %v", err)
	}
	apiProcess, err := os.FindProcess(apiPID)
	if err != nil {
		t.Fatal(err)
	}
	if err := apiProcess.Kill(); err != nil {
		t.Fatalf("kill api: %v", err)
	}

	var statusEnvelope e2eCLIEnvelope
	deadline := time.Now().Add(e2eBootTimeout)
	for time.Now().Before(deadline) {
		output, outputErr := command("status", "--json").Output()
		if outputErr == nil {
			statusEnvelope = parseE2EEnvelope(t, string(output))
			if len(statusEnvelope.RecommendedActions) == 1 &&
				statusEnvelope.RecommendedActions[0].Command == "orbit logs api --json" {
				break
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	if len(statusEnvelope.RecommendedActions) != 1 ||
		statusEnvelope.RecommendedActions[0].Command != "orbit logs api --json" {
		t.Fatalf("status recovery = %+v", statusEnvelope.RecommendedActions)
	}

	doctorOutput, err := command("doctor", "--json").Output()
	if err == nil {
		t.Fatalf("doctor unexpectedly reported a crashed service as healthy:\n%s", doctorOutput)
	}
	doctorEnvelope := parseE2EEnvelope(t, string(doctorOutput))
	if doctorEnvelope.Error == nil ||
		len(doctorEnvelope.RecommendedActions) != 1 ||
		doctorEnvelope.RecommendedActions[0].Command != "orbit logs api --json" ||
		doctorEnvelope.Error.NextCommand != doctorEnvelope.RecommendedActions[0].Command {
		t.Fatalf("doctor recovery = %+v\n%s", doctorEnvelope, doctorOutput)
	}

	logsOutput, err := command("logs", "api", "--json").Output()
	if err != nil {
		t.Fatalf("logs: %v\n%s", err, logsOutput)
	}
	logsEnvelope := parseE2EEnvelope(t, string(logsOutput))
	if len(logsEnvelope.RecommendedActions) != 1 ||
		logsEnvelope.RecommendedActions[0].Command != "orbit restart api --json" ||
		!bytes.Contains(logsEnvelope.Data, []byte("signal: killed")) {
		t.Fatalf("logs recovery = %+v\n%s", logsEnvelope, logsOutput)
	}

	restartOutput, err := command("restart", "api", "--json").Output()
	if err != nil {
		t.Fatalf("restart: %v\n%s", err, restartOutput)
	}
	restartEnvelope := parseE2EEnvelope(t, string(restartOutput))
	var restartData lifecycleJSONData
	if err := json.Unmarshal(restartEnvelope.Data, &restartData); err != nil {
		t.Fatalf("restart data: %v\n%s", err, restartEnvelope.Data)
	}
	if len(restartData.Resources) != 1 ||
		restartData.Resources[0].Name != "api" ||
		restartData.Resources[0].State != "healthy" ||
		restartData.Resources[0].RestartCount != 1 {
		t.Fatalf("restart data = %+v", restartData)
	}
	if redisAfter := containerEvidence(); redisAfter != redisBefore {
		t.Fatalf("healthy redis changed during targeted recovery:\nbefore %s\nafter  %s", redisBefore, redisAfter)
	}

	finalOutput, err := command("status", "--json").Output()
	if err != nil {
		t.Fatalf("final status: %v\n%s", err, finalOutput)
	}
	finalEnvelope := parseE2EEnvelope(t, string(finalOutput))
	for _, action := range finalEnvelope.RecommendedActions {
		if strings.Contains(action.Command, "logs api") || strings.Contains(action.Command, "restart api") {
			t.Fatalf("healthy status retained recovery action: %+v", finalEnvelope.RecommendedActions)
		}
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
	} {
		if !bytes.Contains(human, []byte(evidence)) {
			t.Fatalf("human status missing %q:\n%s", evidence, human)
		}
	}

	failedUp, err := command("up", "api-runtime", "--json").Output()
	if err == nil {
		t.Fatalf("JSON up unexpectedly succeeded:\n%s", failedUp)
	}
	failedEnvelope := parseE2EEnvelope(t, string(failedUp))
	if failedEnvelope.Error == nil || failedEnvelope.Error.Code != "service_start_failed" {
		t.Fatalf("JSON up error = %+v:\n%s", failedEnvelope.Error, failedUp)
	}
	wantRecovery := []string{
		"orbit status --json",
		"orbit logs api-runtime --json",
		"orbit restart api-runtime --json",
	}
	if len(failedEnvelope.RecommendedActions) != len(wantRecovery) {
		t.Fatalf("JSON up recommended_actions = %+v", failedEnvelope.RecommendedActions)
	}
	for i, command := range wantRecovery {
		if failedEnvelope.RecommendedActions[i].Command != command {
			t.Fatalf("JSON up recommended_actions[%d] = %q, want %q", i, failedEnvelope.RecommendedActions[i].Command, command)
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
		Resources []jsonService `json:"resources"`
	}
	if err := json.Unmarshal(envelope.Data, &status); err != nil {
		t.Fatalf("status data: %v\n%s", err, jsonOutput)
	}
	resources := make(map[string]jsonService, len(status.Resources))
	for _, resource := range status.Resources {
		resources[resource.Name] = resource
	}
	if resources["api-runtime"].StateReason == "" {
		t.Fatalf("api-runtime state_reason empty:\n%s", jsonOutput)
	}
	if resources["web-app"].BlockedBy != "api-runtime" {
		t.Fatalf("web-app blocked_by = %q:\n%s", resources["web-app"].BlockedBy, jsonOutput)
	}
	commands := make(map[string]bool, len(envelope.RecommendedActions))
	for _, action := range envelope.RecommendedActions {
		commands[action.Command] = true
	}
	if len(envelope.RecommendedActions) != 1 || !commands["orbit logs api-runtime --json"] {
		t.Fatalf("status recovery is not one logs action: %+v", envelope.RecommendedActions)
	}
	if commands["orbit logs web-app --json"] {
		t.Fatalf("recommended_actions points to dependent without logs: %+v", envelope.RecommendedActions)
	}

	inspectOutput, err := command("inspect", "--json").Output()
	if err != nil {
		t.Fatalf("inspect: %v\n%s", err, inspectOutput)
	}
	inspectEnvelope := parseE2EEnvelope(t, string(inspectOutput))
	var inspectData inspectJSONData
	if err := json.Unmarshal(inspectEnvelope.Data, &inspectData); err != nil {
		t.Fatalf("inspect data: %v\n%s", err, inspectEnvelope.Data)
	}
	if inspectData.Readiness.State != inspectReadinessDegraded {
		t.Fatalf("inspect readiness = %+v", inspectData.Readiness)
	}
	if len(inspectEnvelope.RecommendedActions) != 1 ||
		!hasInspectAction(inspectEnvelope.RecommendedActions, "orbit logs api-runtime --json") {
		t.Fatalf("inspect recommended_actions = %+v", inspectEnvelope.RecommendedActions)
	}
	if hasInspectAction(inspectEnvelope.RecommendedActions, "orbit doctor --json") {
		t.Fatalf("inspect recommended unrelated setup diagnostics: %+v", inspectEnvelope.RecommendedActions)
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
	for _, unwanted := range []string{"Step 3: Environment", "Step 4: Health check", "reading config"} {
		if bytes.Contains(output, []byte(unwanted)) {
			t.Fatalf("init continued into irrelevant setup after sync failure (%q):\n%s", unwanted, output)
		}
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
		Checks   []json.RawMessage `json:"checks"`
		Warnings []string          `json:"warnings"`
		Ready    bool              `json:"ready"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("decode diagnostic data: %v\n%s", err, envelope.Data)
	}
	if data.Ready {
		t.Fatalf("diagnostic data = %s", envelope.Data)
	}
	if len(data.Checks) != 0 {
		t.Fatalf("sync failure ran unrelated health checks: %s", envelope.Data)
	}
	if len(data.Warnings) != 0 {
		t.Fatalf("fatal sync error was duplicated as a warning: %s", envelope.Data)
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

func TestE2E_DoctorDistinguishesMissingNodePackages(t *testing.T) {
	binary := findOrbitBinary(t)
	home := t.TempDir()
	project := filepath.Join(t.TempDir(), "web project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"scripts":{"dev":"vite"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), "node.yaml")
	raw := fmt.Sprintf(`
version: "2"
services:
  web:
    type: node
    path: %q
    command: pnpm dev
`, project)
	if err := os.WriteFile(envPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(binary, "doctor", "--config", envPath, "--json")
	command.Env = append(os.Environ(), "ORBIT_HOME="+home)
	output, err := command.Output()
	if err == nil {
		t.Fatalf("doctor unexpectedly passed without project packages:\n%s", output)
	}
	envelope := parseE2EEnvelope(t, string(output))
	if envelope.OK || envelope.Error == nil || envelope.Error.Code != "checks_failed" {
		t.Fatalf("envelope = %+v:\n%s", envelope, output)
	}
	var data struct {
		Checks []daemon.DoctorCheck `json:"checks"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("decode doctor data: %v\n%s", err, envelope.Data)
	}
	for _, check := range data.Checks {
		if check.Name == "Packages (web)" && check.Status == daemon.CheckFail && check.Message == "project packages are not installed" {
			want := "pnpm --dir " + shellquote.Quote(project) + " install"
			for _, action := range envelope.RecommendedActions {
				if action.Command == want {
					return
				}
			}
			t.Fatalf("recommended_actions missing %q: %+v", want, envelope.RecommendedActions)
		}
	}
	t.Fatalf("package check missing: %+v", data.Checks)
}

func TestE2E_SwitchReportsSelectedEnvironmentPrerequisites(t *testing.T) {
	binary := findOrbitBinary(t)
	home, err := os.MkdirTemp("/tmp", "orb-switch-prereq-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop := exec.Command(binary, "daemon", "stop")
		stop.Env = append(os.Environ(), "ORBIT_HOME="+home)
		_ = stop.Run()
		_ = os.RemoveAll(home)
	})
	project := filepath.Join(t.TempDir(), "web project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"scripts":{"dev":"vite"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	envsDir := filepath.Join(home, "envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`
version: "2"
services:
  web:
    type: node
    path: %q
    command: pnpm dev
`, project)
	if err := os.WriteFile(filepath.Join(envsDir, "node.yaml"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(binary, "switch", "node", "--json")
	command.Env = append(os.Environ(),
		"ORBIT_HOME="+home,
		"ORBIT_DASHBOARD_PORT="+strconv.Itoa(22000+int(randByte())),
	)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("switch failed: %v\n%s", err, output)
	}
	envelope := parseE2EEnvelope(t, string(output))
	if !envelope.OK {
		t.Fatalf("envelope = %+v:\n%s", envelope, output)
	}
	var data struct {
		PrerequisitesReady bool                 `json:"prerequisites_ready"`
		Prerequisites      []daemon.DoctorCheck `json:"prerequisites"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("decode switch data: %v\n%s", err, envelope.Data)
	}
	if data.PrerequisitesReady {
		t.Fatal("switch hid missing project packages")
	}
	for _, check := range data.Prerequisites {
		if check.Name == "Packages (web)" && check.Status == daemon.CheckFail {
			return
		}
	}
	t.Fatalf("prerequisites = %+v", data.Prerequisites)
}

func TestE2E_SwitchReportsProjectRuntimeVersionMismatch(t *testing.T) {
	binary := findOrbitBinary(t)
	home, err := os.MkdirTemp("/tmp", "orb-switch-version-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop := exec.Command(binary, "daemon", "stop")
		stop.Env = append(os.Environ(), "ORBIT_HOME="+home)
		_ = stop.Run()
		_ = os.RemoveAll(home)
	})
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, ".nvmrc"), []byte("999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	envsDir := filepath.Join(home, "envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`
version: "2"
services:
  web:
    type: node
    path: %q
    command: node server.js
`, project)
	if err := os.WriteFile(filepath.Join(envsDir, "versioned.yaml"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	command := func(args ...string) *exec.Cmd {
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(),
			"ORBIT_HOME="+home,
			"ORBIT_DASHBOARD_PORT="+strconv.Itoa(22000+int(randByte())),
		)
		return cmd
	}

	human, err := command("switch", "versioned").CombinedOutput()
	if err != nil {
		t.Fatalf("human switch failed: %v\n%s", err, human)
	}
	for _, evidence := range []string{
		"setup required before `orbit up`",
		"Node.js",
		"web requires 999 (.nvmrc)",
		"Select the project version of Node.js",
	} {
		if !bytes.Contains(human, []byte(evidence)) {
			t.Fatalf("human switch missing %q:\n%s", evidence, human)
		}
	}
	if bytes.Contains(bytes.ToLower(human), []byte("daemon")) {
		t.Fatalf("human switch exposed daemon lifecycle:\n%s", human)
	}

	output, err := command("switch", "versioned", "--json").Output()
	if err != nil {
		t.Fatalf("JSON switch failed: %v\n%s", err, output)
	}
	envelope := parseE2EEnvelope(t, string(output))
	if !envelope.OK {
		t.Fatalf("envelope = %+v:\n%s", envelope, output)
	}
	var data struct {
		PrerequisitesReady bool                 `json:"prerequisites_ready"`
		Prerequisites      []daemon.DoctorCheck `json:"prerequisites"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("decode switch data: %v\n%s", err, envelope.Data)
	}
	if data.PrerequisitesReady {
		t.Fatal("switch reported a mismatched project runtime as ready")
	}
	count := 0
	for _, check := range data.Prerequisites {
		if check.Name == "Node.js" {
			count++
			if check.Status != daemon.CheckFail || !strings.Contains(check.Message, "web requires 999 (.nvmrc)") {
				t.Fatalf("Node.js prerequisite = %+v", check)
			}
		}
	}
	if count != 1 {
		t.Fatalf("Node.js prerequisite count = %d: %+v", count, data.Prerequisites)
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
