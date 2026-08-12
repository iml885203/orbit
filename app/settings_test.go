package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

func TestSettingsEnvironmentVariableCanBeSetBeforeDaemonStarts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	t.Setenv("ORBIT_NAMESPACE", "settings-env-local-test")

	previousJSON := cli.JSONOutput
	cli.JSONOutput = false
	t.Cleanup(func() { cli.JSONOutput = previousJSON })

	if err := runSettingsSetEnv(nil, []string{"API_ROOT", "/work/api"}); err != nil {
		t.Fatal(err)
	}
	got := daemon.LoadSettings(daemon.DefaultSettingsPath())
	if got.GetUserEnv("API_ROOT") != "/work/api" {
		t.Fatalf("API_ROOT = %q", got.GetUserEnv("API_ROOT"))
	}
	if err := runSettingsSetEnv(nil, []string{"invalid-name", "/work/api"}); !errors.Is(err, cli.ErrInvalidArgument) {
		t.Fatalf("invalid name error = %v", err)
	}
}

func TestSettings_CoerceValue(t *testing.T) {
	v, err := coerceSettingsValue("show_history", "true")
	if err != nil || v != true {
		t.Errorf("show_history true: v=%v err=%v", v, err)
	}
	v, err = coerceSettingsValue("show_history", "off")
	if err != nil || v != false {
		t.Errorf("show_history off: v=%v err=%v", v, err)
	}
	if _, err := coerceSettingsValue("show_history", "maybe"); err == nil {
		t.Errorf("expected err for bad bool")
	}
}

func TestSettingsRejectsRetiredGlobalWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	t.Setenv("ORBIT_NAMESPACE", "settings-local-test")

	previousJSON := cli.JSONOutput
	cli.JSONOutput = false
	t.Cleanup(func() { cli.JSONOutput = previousJSON })

	if err := runSettingsSet(nil, []string{"workspace-root", root}); err == nil {
		t.Fatal("settings accepted the retired global workspace root")
	}
	if err := runSettingsSet(nil, []string{"unknown-key", root}); err == nil {
		t.Fatal("settings accepted an unknown key")
	}
}

func TestSettingsListJSONUsesStableEnvelopeWithoutDaemon(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	t.Setenv("ORBIT_NAMESPACE", "settings-list-json-test")
	settings := daemon.LoadSettings(daemon.DefaultSettingsPath())
	if err := settings.Set("workspace_root", root); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetEnvToggle("local/api/FEATURE", true); err != nil {
		t.Fatal(err)
	}

	previousJSON := cli.JSONOutput
	cli.JSONOutput = true
	t.Cleanup(func() { cli.JSONOutput = previousJSON })
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStdout := os.Stdout
	os.Stdout = writeEnd
	t.Cleanup(func() { os.Stdout = previousStdout })

	if err := runSettingsList(nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := writeEnd.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := io.Copy(&output, readEnd); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		OK            bool   `json:"ok"`
		Data          struct {
			Operation string         `json:"operation"`
			Settings  map[string]any `json:"settings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode output %q: %v", output.String(), err)
	}
	if envelope.SchemaVersion != "orbit.cli.v1" || !envelope.OK || envelope.Data.Operation != "settings_list" {
		t.Fatalf("envelope = %+v", envelope)
	}
	if _, exists := envelope.Data.Settings["workspace_root"]; exists {
		t.Fatalf("settings = %+v", envelope.Data.Settings)
	}
	toggles, exists := envelope.Data.Settings["env_toggles"].(map[string]any)
	if !exists || toggles["local/api/FEATURE"] != true {
		t.Fatalf("env_toggles = %#v", envelope.Data.Settings["env_toggles"])
	}
	if _, exists := envelope.Data.Settings["user_env"].(map[string]any); !exists {
		t.Fatalf("user_env must keep an object shape: %#v", envelope.Data.Settings["user_env"])
	}
}

func TestNormalizeSettingsListMapsKeepsStableObjectShape(t *testing.T) {
	settings := map[string]any{}
	normalizeSettingsListMaps(settings)
	if _, ok := settings["env_toggles"].(map[string]bool); !ok {
		t.Fatalf("env_toggles = %#v", settings["env_toggles"])
	}
	if _, ok := settings["user_env"].(map[string]string); !ok {
		t.Fatalf("user_env = %#v", settings["user_env"])
	}
}
