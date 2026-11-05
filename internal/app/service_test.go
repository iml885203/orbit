package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestServiceCmd_ModeSubcommand(t *testing.T) {
	cmd := serviceCmd()
	if cmd.Use != "service" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	found := false
	for _, s := range cmd.Commands() {
		if strings.HasPrefix(s.Use, "mode") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing 'mode' subcommand")
	}
}

func TestServiceMode_RejectsInvalidMode(t *testing.T) {
	if err := validateServiceMode("frontend"); err == nil {
		t.Errorf("expected error for invalid mode")
	}
	if err := validateServiceMode("dev"); err != nil {
		t.Errorf("dev should be valid: %v", err)
	}
	if err := validateServiceMode("container"); err != nil {
		t.Errorf("container should be valid: %v", err)
	}
}

func TestServiceCmd_ArgsValidation(t *testing.T) {
	cmd := serviceCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"mode"})
	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error for missing args")
	}
}
