package app

import (
	"bytes"
	"slices"
	"testing"
)

func TestEnvironmentHelpOnlyShowsListAndSync(t *testing.T) {
	cmd := envCmd()
	var visible []string
	for _, child := range cmd.Commands() {
		if !child.Hidden {
			visible = append(visible, child.Name())
		}
	}
	if want := []string{"list", "sync"}; !slices.Equal(visible, want) {
		t.Fatalf("visible env commands = %v, want %v", visible, want)
	}
}

func TestAdvancedCommandsAreHidden(t *testing.T) {
	commands := []struct {
		name   string
		hidden bool
	}{
		{daemonCmd().Name(), daemonCmd().Hidden},
		{edgeCmd().Name(), edgeCmd().Hidden},
		{historyCmd().Name(), historyCmd().Hidden},
		{inspectCmd().Name(), inspectCmd().Hidden},
		{serviceCmd().Name(), serviceCmd().Hidden},
		{settingsCmd().Name(), settingsCmd().Hidden},
		{tracingCmd().Name(), tracingCmd().Hidden},
	}
	for _, command := range commands {
		if !command.hidden {
			t.Errorf("%s should be hidden from root help", command.name)
		}
	}
}

func TestDailyCommandsUseOptionalTargetInsteadOfDuplicateVerbs(t *testing.T) {
	for _, cmd := range []struct {
		name string
		use  string
	}{
		{"down", downCmd().Use},
		{"open", openCmd().Use},
		{"trace", traceCmd().Use},
	} {
		if cmd.use[0:len(cmd.name)] != cmd.name {
			t.Errorf("%s use = %q", cmd.name, cmd.use)
		}
		if err := map[string]func([]string) error{
			"down":  func(args []string) error { return downCmd().Args(downCmd(), args) },
			"open":  func(args []string) error { return openCmd().Args(openCmd(), args) },
			"trace": func(args []string) error { return traceCmd().Args(traceCmd(), args) },
		}[cmd.name]([]string{"one", "two"}); err == nil {
			t.Errorf("%s accepted more than one target", cmd.name)
		}
	}
}

func TestExecHelpIsNotTreatedAsAContainer(t *testing.T) {
	cmd := execCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("exec --help: %v", err)
	}
	if output.Len() == 0 {
		t.Fatal("exec --help produced no help")
	}
}

func TestUpdateUsesShortPublicName(t *testing.T) {
	if got := selfUpdateCmd().Name(); got != "update" {
		t.Fatalf("update command name = %q", got)
	}
}
