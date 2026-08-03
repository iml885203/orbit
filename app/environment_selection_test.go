package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
)

func TestReadEnvironmentSelectionDistinguishesUnavailableSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	envs := filepath.Join(home, "envs")
	if err := os.MkdirAll(envs, 0755); err != nil {
		t.Fatal(err)
	}
	available := filepath.Join(envs, "renamed.yaml")
	if err := os.WriteFile(available, []byte("version: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(envs, "original.yaml")
	if err := os.WriteFile(daemonsrv.CurrentEnvPath(), []byte(missing+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := readEnvironmentSelection()
	if got.State != environmentSelectionUnavailable {
		t.Fatalf("state = %q, want unavailable", got.State)
	}
	if got.SelectedName != "original" {
		t.Fatalf("selected_name = %q, want original", got.SelectedName)
	}
	if len(got.Environments) != 1 || got.Environments[0].Name != "renamed" {
		t.Fatalf("environments = %+v", got.Environments)
	}
	if got.Environments[0].Selected {
		t.Fatal("renamed environment marked selected")
	}
}

func TestEnvironmentSelectionActionsOfferExactChoices(t *testing.T) {
	actions := environmentSelectionActions(environmentSelection{
		State: environmentSelectionUnavailable,
		Environments: []environmentChoice{
			{Identity: "company/e2e", Name: "e2e"},
			{Identity: "team/e2e", Name: "e2e"},
			{Name: "team dev"},
		},
	})
	if len(actions) != 3 {
		t.Fatalf("actions = %+v", actions)
	}
	if actions[0].Command != "orbit switch company/e2e --json" {
		t.Fatalf("first command = %q", actions[0].Command)
	}
	if actions[1].Command != "orbit switch team/e2e --json" {
		t.Fatalf("second command = %q", actions[1].Command)
	}
	if actions[2].Command != "orbit switch 'team dev' --json" {
		t.Fatalf("third command = %q", actions[2].Command)
	}
}

func TestEnvironmentSelectionActionsSyncWhenNoChoicesExist(t *testing.T) {
	actions := environmentSelectionActions(environmentSelection{
		State:        environmentSelectionUnavailable,
		Environments: []environmentChoice{},
	})
	if len(actions) != 1 || actions[0].Command != "orbit source sync --json" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestInvalidSwitchListsAvailableNamesWithoutChangingSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	envs := filepath.Join(home, "envs")
	if err := os.MkdirAll(envs, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha.yaml", "beta.yaml"} {
		if err := os.WriteFile(filepath.Join(envs, name), []byte(`version: "3"`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	err := runSwitch(nil, []string{"alhpa"})
	if err == nil || !strings.Contains(err.Error(), "Available: alpha, beta") {
		t.Fatalf("switch error = %v", err)
	}
	var output bytes.Buffer
	if writeErr := cli.WriteJSONError(&output, "orbit switch alhpa --json", err); writeErr != nil {
		t.Fatal(writeErr)
	}
	var envelope struct {
		RecommendedActions []cli.JSONAction `json:"recommended_actions"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.RecommendedActions) != 1 ||
		envelope.RecommendedActions[0].Command != "orbit env list --json" {
		t.Fatalf("actions = %+v", envelope.RecommendedActions)
	}
	if _, err := os.Stat(daemonsrv.CurrentEnvPath()); !os.IsNotExist(err) {
		t.Fatalf("invalid switch changed environment selection: %v", err)
	}
}

func TestEnvironmentSelectionErrorUsesStableJSONRecovery(t *testing.T) {
	err := newEnvironmentSelectionRequiredError(environmentSelection{
		State: environmentSelectionUnavailable,
		Environments: []environmentChoice{{
			Name: "renamed",
		}},
	})
	var buf bytes.Buffer
	if writeErr := cli.WriteJSONError(&buf, "orbit up --json", err); writeErr != nil {
		t.Fatal(writeErr)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		RecommendedActions []cli.JSONAction `json:"recommended_actions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "environment_selection_required" {
		t.Fatalf("error code = %q", envelope.Error.Code)
	}
	if len(envelope.RecommendedActions) != 1 ||
		envelope.RecommendedActions[0].Command != "orbit switch renamed --json" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
}
