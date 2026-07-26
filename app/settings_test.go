package app

import (
	"strings"
	"testing"
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
		{"sql-server-image", "sql_server_image", false},
		{"sql-server-pull-policy", "sql_server_pull_policy", false},
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
	v, err = coerceSettingsValue("sql_server_image", "example.db:latest")
	if err != nil || v != "example.db:latest" {
		t.Errorf("string passthrough: v=%v err=%v", v, err)
	}
	if _, err := coerceSettingsValue("show_history", "maybe"); err == nil {
		t.Errorf("expected err for bad bool")
	}
}
