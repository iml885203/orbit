package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/internal/envsource"
)

const initSourceDemoURL = "https://invalid.example/distribution-demo.git"

// TestInitSourceHelper runs `orbit init` in a child process so package-level
// flag state and ORBIT_HOME stay isolated per scenario.
func TestInitSourceHelper(t *testing.T) {
	raw := os.Getenv("ORBIT_INIT_SOURCE_HELPER_ARGS")
	if raw == "" {
		t.Skip("helper process only")
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatal(err)
	}
	cli.JSONOutput = os.Getenv("ORBIT_INIT_SOURCE_HELPER_JSON") == "1"
	distribution.EnvRepoURL = initSourceDemoURL
	distribution.EnvRepoRef = "v-demo"
	distribution.DefaultEnv = "dev.yaml"
	cmd := initCmd()
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

type initSourceRun struct {
	stdout string
	stderr string
	err    error
}

func runInitSourceHelper(t *testing.T, orbitHome string, jsonOutput bool, args ...string) initSourceRun {
	t.Helper()
	return runInitSourceHelperWithEnv(t, orbitHome, jsonOutput, nil, args...)
}

// runInitSourceHelperWithEnv appends env after the isolation defaults, so an
// entry such as ORBIT_ENV_REPO_URL overrides the cleared value.
func runInitSourceHelperWithEnv(t *testing.T, orbitHome string, jsonOutput bool, env []string, args ...string) initSourceRun {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInitSourceHelper$")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"ORBIT_INIT_SOURCE_HELPER_ARGS="+string(encoded),
		"ORBIT_HOME="+orbitHome,
		"ORBIT_ENV_REPO_URL=",
		"ORBIT_ENV_REPO_REF=",
	)
	if jsonOutput {
		cmd.Env = append(cmd.Env, "ORBIT_INIT_SOURCE_HELPER_JSON=1")
	}
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	return initSourceRun{stdout: stdout.String(), stderr: stderr.String(), err: runErr}
}

func writeInitSourceEnvs(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "envs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "envs", "dev.yaml"), []byte("version: \"3\"\nservices: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeInitSourceGitRepo(t *testing.T, branches ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	writeInitSourceEnvs(t, dir)
	run("add", ".")
	run("commit", "-q", "-m", "seed")
	for _, branch := range branches {
		run("branch", branch)
	}
	return "file://" + dir
}

func registerInitSource(t *testing.T, orbitHome string, source envsource.Source) {
	t.Helper()
	registry, err := envsource.Load(envsource.RegistryPath(orbitHome))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(source); err != nil {
		t.Fatal(err)
	}
}

func storedInitSource(t *testing.T, orbitHome, name string) envsource.Source {
	t.Helper()
	registry, err := envsource.Load(envsource.RegistryPath(orbitHome))
	if err != nil {
		t.Fatal(err)
	}
	source, err := registry.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func decodeInitSourceData(t *testing.T, run initSourceRun) map[string]any {
	t.Helper()
	if run.err != nil {
		t.Fatalf("init failed: %v\nstderr:\n%s\nstdout:\n%s", run.err, run.stderr, run.stdout)
	}
	var envelope cli.JSONEnvelope
	if err := json.Unmarshal([]byte(run.stdout), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, run.stdout)
	}
	if !envelope.OK {
		t.Fatalf("ok = false: %+v", envelope.Error)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T", envelope.Data)
	}
	return data
}

func TestInitByRegisteredLocalSourceNameKeepsItsDefinition(t *testing.T) {
	orbitHome := t.TempDir()
	local := t.TempDir()
	writeInitSourceEnvs(t, local)
	registerInitSource(t, orbitHome, envsource.Source{Name: "team", Type: envsource.TypeLocal, Path: local})

	data := decodeInitSourceData(t, runInitSourceHelper(t, orbitHome, true, "--yes", "--source", "team", "--env", "dev"))

	if data["env_source"] != envsource.TypeLocal || data["env_repo_url"] != nil {
		t.Fatalf("env_source = %v, env_repo_url = %v; want the registered local source", data["env_source"], data["env_repo_url"])
	}
	stored := storedInitSource(t, orbitHome, "team")
	if stored.Type != envsource.TypeLocal || stored.Path != local || stored.URL != "" {
		t.Fatalf("stored source = %+v, want local %s", stored, local)
	}
}

func TestInitByRegisteredGitSourceNameKeepsItsDefinition(t *testing.T) {
	orbitHome := t.TempDir()
	url := makeInitSourceGitRepo(t)
	registerInitSource(t, orbitHome, envsource.Source{Name: "team", Type: envsource.TypeGit, URL: url, Ref: "main"})

	run := runInitSourceHelper(t, orbitHome, false, "--yes", "--source", "team", "--env", "dev")
	if run.err != nil {
		t.Fatalf("init failed: %v\nstderr:\n%s\nstdout:\n%s", run.err, run.stderr, run.stdout)
	}
	for _, demo := range []string{"Quickstart", "Orbit demo", "invalid.example"} {
		if strings.Contains(run.stdout, demo) {
			t.Fatalf("init of a registered source printed %q:\n%s", demo, run.stdout)
		}
	}
	stored := storedInitSource(t, orbitHome, "team")
	if stored.Type != envsource.TypeGit || stored.URL != url || stored.Ref != "main" {
		t.Fatalf("stored source = %+v, want git %s @ main", stored, url)
	}
}

func TestInitJSONByRegisteredGitSourceNameReportsThatSource(t *testing.T) {
	orbitHome := t.TempDir()
	url := makeInitSourceGitRepo(t)
	registerInitSource(t, orbitHome, envsource.Source{Name: "team", Type: envsource.TypeGit, URL: url, Ref: "main"})

	data := decodeInitSourceData(t, runInitSourceHelper(t, orbitHome, true, "--yes", "--source", "team", "--env", "dev"))

	if data["env_source"] != envsource.TypeGit || data["env_repo_url"] != url || data["env_repo_ref"] != "main" {
		t.Fatalf("env_source = %v, env_repo_url = %v, env_repo_ref = %v; want the registered git source",
			data["env_source"], data["env_repo_url"], data["env_repo_ref"])
	}
}

func TestInitWithoutFlagsReusesTheFirstRegisteredSource(t *testing.T) {
	orbitHome := t.TempDir()
	local := t.TempDir()
	writeInitSourceEnvs(t, local)
	registerInitSource(t, orbitHome, envsource.Source{Name: "local-team", Type: envsource.TypeLocal, Path: local})

	data := decodeInitSourceData(t, runInitSourceHelper(t, orbitHome, true, "--yes", "--env", "dev"))

	if data["env_source"] != envsource.TypeLocal || data["env_repo_url"] != nil {
		t.Fatalf("env_source = %v, env_repo_url = %v; want the first registered source", data["env_source"], data["env_repo_url"])
	}
	if stored := storedInitSource(t, orbitHome, "local-team"); stored.Path != local {
		t.Fatalf("stored source = %+v, want local %s", stored, local)
	}
}

func TestInitByRegisteredSourceNameKeepsItWhenItsSyncFails(t *testing.T) {
	orbitHome := t.TempDir()
	unreachable := "file://" + filepath.Join(t.TempDir(), "missing-repo")
	registerInitSource(t, orbitHome, envsource.Source{Name: "team", Type: envsource.TypeGit, URL: unreachable, Ref: "main"})

	run := runInitSourceHelper(t, orbitHome, true, "--yes", "--source", "team", "--env", "dev")

	if run.err == nil {
		t.Fatalf("init succeeded against an unreachable source:\n%s", run.stdout)
	}
	if strings.Contains(run.stdout+run.stderr, "invalid.example") {
		t.Fatalf("failed init fell back to the demo repo:\nstdout:\n%s\nstderr:\n%s", run.stdout, run.stderr)
	}
	stored := storedInitSource(t, orbitHome, "team")
	if stored.Type != envsource.TypeGit || stored.URL != unreachable || stored.Ref != "main" {
		t.Fatalf("stored source = %+v, want git %s @ main", stored, unreachable)
	}
}

func TestInitByUnregisteredSourceNameRegistersTheDefaultRepository(t *testing.T) {
	orbitHome := t.TempDir()
	url := makeInitSourceGitRepo(t, "release")

	data := decodeInitSourceData(t, runInitSourceHelperWithEnv(t, orbitHome, true, []string{"ORBIT_ENV_REPO_URL=" + url},
		"--yes", "--source", "fresh", "--ref", "release", "--env", "dev"))

	if data["env_repo_url"] != url || data["env_repo_ref"] != "release" {
		t.Fatalf("env_repo_url = %v, env_repo_ref = %v; want default repository %s @ release",
			data["env_repo_url"], data["env_repo_ref"], url)
	}
	if stored := storedInitSource(t, orbitHome, "fresh"); stored.URL != url || stored.Ref != "release" {
		t.Fatalf("stored source = %+v, want %s @ release", stored, url)
	}
}

func TestInitByRegisteredSourceNameAppliesRefAndWorkspaceOverrides(t *testing.T) {
	orbitHome := t.TempDir()
	workspace := t.TempDir()
	url := makeInitSourceGitRepo(t, "release")
	registerInitSource(t, orbitHome, envsource.Source{Name: "team", Type: envsource.TypeGit, URL: url, Ref: "main"})

	data := decodeInitSourceData(t, runInitSourceHelper(t, orbitHome, true,
		"--yes", "--source", "team", "--ref", "release", "--workspace", workspace, "--env", "dev"))

	if data["env_repo_url"] != url || data["env_repo_ref"] != "release" {
		t.Fatalf("env_repo_url = %v, env_repo_ref = %v; want %s @ release", data["env_repo_url"], data["env_repo_ref"], url)
	}
	stored := storedInitSource(t, orbitHome, "team")
	if stored.URL != url || stored.Ref != "release" {
		t.Fatalf("stored source = %+v, want %s @ release", stored, url)
	}
	wantWorkspace, err := envsource.NormalizeExistingDirectory(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Workspace != wantWorkspace {
		t.Fatalf("stored workspace = %q, want %q", stored.Workspace, wantWorkspace)
	}
}

func TestInitByRegisteredLocalSourceNameRejectsRef(t *testing.T) {
	orbitHome := t.TempDir()
	local := t.TempDir()
	writeInitSourceEnvs(t, local)
	registerInitSource(t, orbitHome, envsource.Source{Name: "team", Type: envsource.TypeLocal, Path: local})

	t.Setenv("ORBIT_HOME", orbitHome)
	previous := []string{initSource, initEnvRepo, initPath, initEnvRef}
	previousYes, previousJSON := initYes, cli.JSONOutput
	t.Cleanup(func() {
		initSource, initEnvRepo, initPath, initEnvRef = previous[0], previous[1], previous[2], previous[3]
		initYes, cli.JSONOutput = previousYes, previousJSON
	})
	initSource, initEnvRepo, initPath, initEnvRef = "team", "", "", "main"
	initYes, cli.JSONOutput = true, true

	if err := runInit(nil, nil); !errors.Is(err, cli.ErrInvalidArgument) {
		t.Fatalf("runInit error = %v, want invalid_argument for --ref on a local source", err)
	}
	if stored := storedInitSource(t, orbitHome, "team"); stored.Type != envsource.TypeLocal || stored.Path != local {
		t.Fatalf("stored source changed: %+v", stored)
	}
}

func TestInitWithLocalPathDoesNotClaimTheDemo(t *testing.T) {
	orbitHome := t.TempDir()
	local := t.TempDir()
	writeInitSourceEnvs(t, local)

	run := runInitSourceHelper(t, orbitHome, false, "--yes", "--source", "mine", "--path", local, "--env", "dev")

	if run.err != nil {
		t.Fatalf("init failed: %v\nstderr:\n%s\nstdout:\n%s", run.err, run.stderr, run.stdout)
	}
	for _, demo := range []string{"Quickstart", "Orbit demo", "Demo environment"} {
		if strings.Contains(run.stdout, demo) {
			t.Fatalf("--path init printed %q:\n%s", demo, run.stdout)
		}
	}
	if !strings.Contains(run.stdout, "Step 1: Environment source") {
		t.Fatalf("--path init did not announce its environment source:\n%s", run.stdout)
	}
}

// The init JSON reports the resolved workspace through source.workspace only,
// so a workspace found during init (here from WORKSPACE_ROOT) must land in
// that field, not just in the registry.
func TestInitJSONReportsAWorkspaceResolvedDuringInit(t *testing.T) {
	orbitHome := t.TempDir()
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(local, "envs"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := "version: \"3\"\nservices:\n  api:\n    type: node\n    path: ${WORKSPACE_ROOT}/api\n    command: node server.js\n"
	if err := os.WriteFile(filepath.Join(local, "envs", "dev.yaml"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "api"), 0o755); err != nil {
		t.Fatal(err)
	}

	run := runInitSourceHelperWithEnv(t, orbitHome, true, []string{"WORKSPACE_ROOT=" + workspace},
		"--yes", "--source", "mine", "--path", local, "--env", "dev")

	data := decodeInitSourceData(t, run)
	source, ok := data["source"].(map[string]any)
	if !ok || source["workspace"] != workspace {
		t.Fatalf("data.source = %v, want workspace %q", data["source"], workspace)
	}
	if stored := storedInitSource(t, orbitHome, "mine"); stored.Workspace != workspace {
		t.Fatalf("stored workspace = %q, want %q", stored.Workspace, workspace)
	}
}

// When the sync fails and init continues with the cached environments, the
// JSON still names the source and its workspace, as workspace_root used to.
func TestInitJSONReportsTheWorkspaceWhenTheSyncFails(t *testing.T) {
	orbitHome := t.TempDir()
	local := t.TempDir()
	writeInitSourceEnvs(t, local)
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first := runInitSourceHelper(t, orbitHome, true, "--yes", "--source", "mine", "--path", local, "--workspace", workspace, "--env", "dev")
	decodeInitSourceData(t, first)
	if err := os.RemoveAll(local); err != nil {
		t.Fatal(err)
	}

	second := runInitSourceHelper(t, orbitHome, true, "--yes", "--source", "mine", "--env", "dev")

	var envelope struct {
		Data struct {
			Source *struct {
				Workspace string `json:"workspace"`
			} `json:"source"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(second.stdout), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, second.stdout)
	}
	if envelope.Data.Source == nil || envelope.Data.Source.Workspace != workspace {
		t.Fatalf("data.source = %+v, want workspace %q\nstdout:\n%s", envelope.Data.Source, workspace, second.stdout)
	}
}
