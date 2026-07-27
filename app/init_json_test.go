package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/internal/envsync"
)

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
	envsDir := filepath.Join(root, "envs")
	if err := os.Mkdir(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := []byte("version: \"2\"\nservices: {}\n")
	if err := os.WriteFile(filepath.Join(envsDir, "quickstart.yaml"), config, 0o644); err != nil {
		t.Fatal(err)
	}
	orbitHome := filepath.Join(t.TempDir(), "orbit-home")

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

func TestInitCompletionNeverClaimsIncompleteSetupSucceeded(t *testing.T) {
	tests := []struct {
		name        string
		result      initResult
		wantHeading string
		wantCommand string
		wantReady   bool
	}{
		{
			name:        "environment sync failed",
			result:      initResult{},
			wantHeading: "Setup is incomplete",
			wantCommand: "orbit init",
		},
		{
			name:        "required tool missing",
			result:      initResult{ActiveEnv: "dev.yaml"},
			wantHeading: "Setup saved, but prerequisites are missing",
			wantCommand: "orbit doctor",
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
			got := buildInitCompletion(test.result)
			if got.Heading != test.wantHeading {
				t.Errorf("heading = %q, want %q", got.Heading, test.wantHeading)
			}
			if got.HumanCommand != test.wantCommand {
				t.Errorf("human command = %q, want %q", got.HumanCommand, test.wantCommand)
			}
			if got.Ready != test.wantReady {
				t.Errorf("ready = %v, want %v", got.Ready, test.wantReady)
			}
		})
	}
}
