package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/envsource"
	"github.com/iml885203/orbit/internal/envsync"
)

func TestInitReusesExistingFirstSource(t *testing.T) {
	if os.Getenv("ORBIT_INIT_EXISTING_SOURCE_HELPER") == "1" {
		cli.JSONOutput = true
		initYes = true
		initEnvRepo, initEnvRef, initEnvName, initSource, initPath, initWorkspace = "", "", "dev", "", "", ""
		distribution.EnvRepoURL = "https://invalid.example/distribution.git"
		if err := runInit(nil, nil); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}
	orbitHome := t.TempDir()
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(local, "envs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "envs", "dev.yaml"), []byte("version: \"3\"\nservices: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	registry, err := envsource.Load(envsource.RegistryPath(orbitHome))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "local-team", Type: envsource.TypeLocal, Path: local}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInitReusesExistingFirstSource$")
	cmd.Env = append(os.Environ(), "ORBIT_INIT_EXISTING_SOURCE_HELPER=1", "ORBIT_HOME="+orbitHome)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("helper failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "invalid.example") || !strings.Contains(stdout.String(), "local-team") {
		t.Fatalf("init replaced existing first source: %s", stdout.String())
	}
}

func TestInitJSONOutputIsOneParseableEnvelope(t *testing.T) {
	if os.Getenv("ORBIT_INIT_JSON_HELPER") == "1" {
		cli.JSONOutput = true
		initYes = true
		initEnvRepo = ""
		initEnvName = "quickstart"
		configFile = ""
		distribution.DefaultEnv = "quickstart.yaml"
		if err := runInit(nil, nil); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}

	root := t.TempDir()
	orbitHome := filepath.Join(t.TempDir(), "orbit-home")
	envsDir := filepath.Join(orbitHome, "envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := []byte("version: \"3\"\nservices: {}\n")
	if err := os.WriteFile(filepath.Join(envsDir, "quickstart.yaml"), config, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInitJSONOutputIsOneParseableEnvelope$")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"ORBIT_INIT_JSON_HELPER=1",
		"ORBIT_HOME="+orbitHome,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("helper failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}

	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var envelope cli.JSONEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode init envelope: %v\nstdout:\n%s", err, stdout.String())
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout contains content after first envelope: %v\nstdout:\n%s", err, stdout.String())
	}
	if !envelope.OK {
		t.Fatalf("ok = false, error = %+v", envelope.Error)
	}
	if envelope.Command == "" || envelope.Data == nil {
		t.Fatalf("incomplete envelope: %+v", envelope)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want object", envelope.Data)
	}
	if data["active_env"] != "quickstart" {
		t.Fatalf("active_env = %q, want logical name without file extension", data["active_env"])
	}
}

func TestInitIgnoresIncidentalLocalEnvsDirectory(t *testing.T) {
	if os.Getenv("ORBIT_INIT_ZERO_PROMPT_HELPER") == "1" {
		cli.JSONOutput = false
		initYes = true
		initEnvRepo = ""
		initEnvName = ""
		configFile = ""
		distribution.DefaultEnv = "quickstart.yaml"
		if err := runInit(nil, nil); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}

	root := t.TempDir()
	envsDir := filepath.Join(root, "envs")
	if err := os.Mkdir(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envsDir, "trap.yaml"), []byte("not: valid: yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orbitHome := filepath.Join(t.TempDir(), "orbit-home")
	managedEnvsDir := filepath.Join(orbitHome, "envs")
	if err := os.MkdirAll(managedEnvsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managedEnvsDir, "quickstart.yaml"), []byte("version: \"3\"\nservices: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInitIgnoresIncidentalLocalEnvsDirectory$")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"ORBIT_INIT_ZERO_PROMPT_HELPER=1",
		"ORBIT_HOME="+orbitHome,
	)
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("helper failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}
	for _, irrelevant := range []string{"Workspace root", "Git URL", "Project workspace"} {
		if strings.Contains(stdout.String(), irrelevant) {
			t.Fatalf("init exposed irrelevant %q:\n%s", irrelevant, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "Syncing local") ||
		!strings.Contains(stdout.String(), "Environment: quickstart") {
		t.Fatalf("init used cwd/envs instead of the managed environment:\n%s", stdout.String())
	}
	settings := daemon.LoadSettings(filepath.Join(orbitHome, "settings.json"))
	if root := settings.Get("workspace_root"); root != "" {
		t.Fatalf("init persisted unrelated workspace %q", root)
	}
}

func TestInitJSONRequiresNonInteractiveMode(t *testing.T) {
	previousJSON := cli.JSONOutput
	previousYes := initYes
	t.Cleanup(func() {
		cli.JSONOutput = previousJSON
		initYes = previousYes
	})
	cli.JSONOutput = true
	initYes = false

	if err := runInit(nil, nil); err == nil {
		t.Fatal("runInit returned nil without --yes")
	}
}

func TestIncompleteInitJSONIsFailureWithRecoveryActions(t *testing.T) {
	var buf bytes.Buffer
	result := initResult{Ready: false, Warnings: []string{"env sync failed"}}
	syncFailure := envRepoSyncError(&envsync.CloneError{
		URL: "https://github.com/example/private-env.git",
		Err: errors.New("exit status 128"),
	})
	failure := initFailure(result, syncFailure)
	if err := cli.WriteJSONFailure(&buf, "orbit init --yes --json", result, failure, initRecommendedActions(result)); err != nil {
		t.Fatal(err)
	}
	envelope := decodeEnvelope(t, buf.Bytes())
	if envelope.OK || envelope.Error == nil || envelope.Error.Code != "env_repo_access" {
		t.Fatalf("envelope = %+v", envelope)
	}
	commands := make(map[string]bool, len(envelope.RecommendedActions))
	for _, action := range envelope.RecommendedActions {
		commands[action.Command] = true
	}
	for _, command := range []string{"gh auth login", "gh auth setup-git", "orbit init --yes --json"} {
		if !commands[command] {
			t.Fatalf("recommended_actions missing %q: %+v", command, envelope.RecommendedActions)
		}
	}
}

func TestMissingGitHubRepoDoesNotRecommendAuthenticationOrBlindRetry(t *testing.T) {
	result := initResult{Ready: false, Warnings: []string{"env sync failed"}}
	syncFailure := envRepoSyncError(&envsync.CloneError{
		URL:    "https://github.com/example/typo-env.git",
		Err:    errors.New("exit status 128"),
		Output: "fatal: could not read Username for 'https://github.com': Device not configured",
	})
	failure := initFailure(result, syncFailure)

	var buf bytes.Buffer
	if err := cli.WriteJSONFailure(
		&buf,
		"orbit init --yes --json",
		result,
		failure,
		initFailureRecommendedActions(result, failure),
	); err != nil {
		t.Fatal(err)
	}
	envelope := decodeEnvelope(t, buf.Bytes())
	if envelope.Error == nil || envelope.Error.Code != "env_repo_unavailable" {
		t.Fatalf("error envelope = %+v", envelope)
	}
	if len(envelope.RecommendedActions) != 0 {
		t.Fatalf("ambiguous failure recommends an assumptive action: %+v", envelope.RecommendedActions)
	}
	for _, action := range envelope.RecommendedActions {
		if action.Command == "gh auth login" || action.Command == "gh auth setup-git" {
			t.Fatalf("missing repository was treated as known-private: %+v", envelope.RecommendedActions)
		}
	}
}

func TestInitRecommendedActionsLeadToNextUsefulCommand(t *testing.T) {
	tests := []struct {
		name   string
		result initResult
		want   string
	}{
		{name: "missing env", result: initResult{}, want: "orbit init --yes --json"},
		{name: "checks failed", result: initResult{ActiveEnv: "dev.yaml"}, want: "orbit doctor --json"},
		{name: "ready", result: initResult{ActiveEnv: "dev.yaml", Ready: true}, want: "orbit up --json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actions := initRecommendedActions(test.result)
			if len(actions) != 1 || actions[0].Command != test.want {
				t.Fatalf("actions = %+v, want %q", actions, test.want)
			}
		})
	}
}

func TestInitWorkspaceFailureLeadsDirectlyToWorkspaceSetting(t *testing.T) {
	result := initResult{
		ActiveEnv: "dev",
		Checks: []daemon.DoctorCheck{{
			Name:    "Working directory (api)",
			Status:  daemon.CheckFail,
			Message: "workspace variable is unresolved",
			Hint:    `run: orbit settings set workspace-root "$PWD"`,
		}},
	}

	actions := initRecommendedActions(result)
	if len(actions) != 1 || actions[0].Command != `orbit settings set workspace-root "$PWD" --json` {
		t.Fatalf("actions = %+v", actions)
	}
	if failure := initFailure(result, nil); !errors.Is(failure, cli.ErrServiceWorkingDir) {
		t.Fatalf("failure = %v, want service working directory classification", failure)
	}
	completion := buildInitCompletion(result, nil)
	if completion.HumanCommand != `orbit settings set workspace-root "$PWD"` {
		t.Fatalf("human command = %q", completion.HumanCommand)
	}
}

func TestInitCustomVariableFailureLeadsToPersistentSetting(t *testing.T) {
	result := initResult{
		ActiveEnv: "dev",
		Checks: []daemon.DoctorCheck{{
			Name:    "Working directory (api)",
			Status:  daemon.CheckFail,
			Message: "path variable API_ROOT is unresolved",
			Hint:    `run: orbit settings set-env API_ROOT "$PWD"`,
		}},
	}

	actions := initRecommendedActions(result)
	if len(actions) != 1 || actions[0].Command != `orbit settings set-env API_ROOT "$PWD" --json` {
		t.Fatalf("actions = %+v", actions)
	}
	completion := buildInitCompletion(result, nil)
	if completion.HumanCommand != `orbit settings set-env API_ROOT "$PWD"` {
		t.Fatalf("human command = %q", completion.HumanCommand)
	}
}

func TestInitCompletionNeverClaimsIncompleteSetupSucceeded(t *testing.T) {
	tests := []struct {
		name        string
		result      initResult
		syncFailure error
		wantHeading string
		wantCommand string
		wantReady   bool
	}{
		{
			name:        "repository may be missing or private",
			result:      initResult{},
			syncFailure: cli.NewEnvRepoUnavailableError("repository not found"),
			wantHeading: "Setup is incomplete",
			wantCommand: "",
		},
		{
			name:        "environment sync failed",
			result:      initResult{},
			wantHeading: "Setup is incomplete",
			wantCommand: "orbit init",
		},
		{
			name: "required tool missing",
			result: initResult{
				ActiveEnv: "dev.yaml",
				Checks: []daemon.DoctorCheck{{
					Name:    "Python",
					Status:  daemon.CheckFail,
					Message: "python3 not found on PATH (required by api)",
					Hint:    "Install Python 3: https://www.python.org/downloads/",
				}},
			},
			wantHeading: "Setup saved — one prerequisite remains",
			wantCommand: "Install Python 3: https://www.python.org/downloads/",
		},
		{
			name:        "ready",
			result:      initResult{ActiveEnv: "dev.yaml", Ready: true},
			wantHeading: "Setup complete!",
			wantCommand: "orbit up",
			wantReady:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildInitCompletion(test.result, test.syncFailure)
			if got.Heading != test.wantHeading {
				t.Errorf("heading = %q, want %q", got.Heading, test.wantHeading)
			}
			if got.HumanCommand != test.wantCommand {
				t.Errorf("human command = %q, want %q", got.HumanCommand, test.wantCommand)
			}
			if got.Ready != test.wantReady {
				t.Errorf("ready = %v, want %v", got.Ready, test.wantReady)
			}
			if test.name == "required tool missing" && got.FollowUp != "orbit up" {
				t.Errorf("follow-up = %q, want orbit up", got.FollowUp)
			}
		})
	}
}

func TestInitCompletionDoesNotHideMultiplePrerequisitesBehindOneHint(t *testing.T) {
	result := initResult{
		ActiveEnv: "dev.yaml",
		Checks: []daemon.DoctorCheck{
			{Name: "Python", Status: daemon.CheckFail, Hint: "Install Python 3"},
			{Name: "Docker", Status: daemon.CheckFail, Hint: "Start Docker"},
		},
	}
	got := buildInitCompletion(result, nil)
	if got.Heading != "Setup saved, but prerequisites are missing" {
		t.Fatalf("heading = %q", got.Heading)
	}
	if got.HumanCommand != "orbit doctor" || got.FollowUp != "" {
		t.Fatalf("completion = %+v, want doctor without an assumptive follow-up", got)
	}
}

func TestInitMissingExternalPrerequisiteKeepsJSONRecoveryExecutable(t *testing.T) {
	result := initResult{
		ActiveEnv: "dev.yaml",
		Checks: []daemon.DoctorCheck{{
			Name:   "Python",
			Status: daemon.CheckFail,
			Hint:   "Install Python 3: https://www.python.org/downloads/",
		}},
	}
	actions := initRecommendedActions(result)
	if len(actions) != 1 || actions[0].Command != "orbit doctor --json" {
		t.Fatalf("actions = %+v", actions)
	}
}
