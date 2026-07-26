package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestEdgeCmd_DetachAndAttach(t *testing.T) {
	cmd := edgeCmd()
	if cmd.Use != "edge" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	names := map[string]bool{}
	for _, s := range cmd.Commands() {
		names[strings.Fields(s.Use)[0]] = true
	}
	if !names["detach"] || !names["attach"] {
		t.Errorf("missing subcommands: %v", names)
	}
}

func TestEdgeCmd_ArgsValidation(t *testing.T) {
	cmd := edgeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"detach"})
	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error for missing args")
	}
}
