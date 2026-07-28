package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

func TestSettingsCmd_Subcommands(t *testing.T) {
	cmd := settingsCmd()
	if cmd.Use != "settings" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	names := map[string]bool{}
	for _, s := range cmd.Commands() {
		names[strings.Fields(s.Use)[0]] = true
	}
	if !names["set"] || !names["list"] {
		t.Errorf("missing subcommands: %v", names)
	}
}

func TestSettings_TranslateKey(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		isErr bool
	}{
		{"workspace-root", "workspace_root", false},
		{"show-history", "show_history", false},
		{"unknown-key", "", true},
	}
	for _, c := range cases {
		got, err := translateSettingsKey(c.in)
		if (err != nil) != c.isErr {
			t.Errorf("translateSettingsKey(%q) err=%v wantErr=%v", c.in, err, c.isErr)
		}
		if err == nil && got != c.want {
			t.Errorf("translateSettingsKey(%q) = %q want %q", c.in, got, c.want)
		}
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
	v, err = coerceSettingsValue("workspace_root", "/work/project")
	if err != nil || v != "/work/project" {
		t.Errorf("string passthrough: v=%v err=%v", v, err)
	}
	if _, err := coerceSettingsValue("show_history", "maybe"); err == nil {
		t.Errorf("expected err for bad bool")
	}
}

func TestNormalizeWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	got, err := normalizeWorkspaceRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("workspace root = %q, want %q", got, root)
	}

	file := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeWorkspaceRoot(file); err == nil {
		t.Fatal("file accepted as workspace root")
	}
	if _, err := normalizeWorkspaceRoot(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing workspace root accepted")
	}
}

func TestSettingsWorkspaceRootCanBeSetBeforeDaemonStarts(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	t.Setenv("ORBIT_NAMESPACE", "settings-local-test")

	previousJSON := cli.JSONOutput
	cli.JSONOutput = false
	t.Cleanup(func() { cli.JSONOutput = previousJSON })

	if err := runSettingsSet(nil, []string{"workspace-root", root}); err != nil {
		t.Fatal(err)
	}
	got := daemon.LoadSettings(daemon.DefaultSettingsPath())
	if got.WorkspaceRoot != root {
		t.Fatalf("workspace root = %q, want %q", got.WorkspaceRoot, root)
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
	if envelope.Data.Settings["workspace_root"] != root {
		t.Fatalf("settings = %+v", envelope.Data.Settings)
	}
}
