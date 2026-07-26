package app

import (
	"strings"
	"testing"
)

func TestEnvCmd_HasToggleSubcommand(t *testing.T) {
	cmd := envCmd()
	for _, s := range cmd.Commands() {
		if strings.HasPrefix(s.Use, "toggle") {
			return
		}
	}
	t.Errorf("env cmd missing 'toggle' subcommand")
}

func TestEnvToggle_ParseOnOff(t *testing.T) {
	cases := []struct {
		in    string
		want  bool
		isErr bool
	}{
		{"on", true, false},
		{"off", false, false},
		{"true", true, false},
		{"false", false, false},
		{"yes", false, true},
		{"", false, true},
	}
	for _, c := range cases {
		got, err := parseOnOff(c.in)
		if (err != nil) != c.isErr {
			t.Errorf("parseOnOff(%q) err=%v wantErr=%v", c.in, err, c.isErr)
		}
		if err == nil && got != c.want {
			t.Errorf("parseOnOff(%q) = %v want %v", c.in, got, c.want)
		}
	}
}
