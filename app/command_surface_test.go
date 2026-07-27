package app

import (
	"bytes"
	"slices"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/extension"
	"github.com/spf13/cobra"
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

func TestUninstallUsesShortPublicName(t *testing.T) {
	if got := uninstallCmd().Name(); got != "uninstall" {
		t.Fatalf("uninstall command name = %q", got)
	}
}

func TestDatabaseCommandsRequireMatchingDaemonConfig(t *testing.T) {
	root := &cobra.Command{Use: "orbit"}
	db := &cobra.Command{Use: "db"}
	list := &cobra.Command{Use: "list"}
	status := &cobra.Command{Use: "status"}
	root.AddCommand(db, status)
	db.AddCommand(list)

	if !commandRequiresMatchingDaemonConfig(list) {
		t.Fatal("nested db command did not require matching daemon config")
	}
	if commandRequiresMatchingDaemonConfig(status) {
		t.Fatal("unrelated command required the db-specific guard")
	}
}

func TestContextualHelpHidesCommandsWithoutASelectedEnvironment(t *testing.T) {
	root := commandVisibilityTestRoot()
	applyContextualCommandVisibility(root, nil, []extension.Extension{{
		CommandVisibility: func(*config.Config) map[string]bool {
			return map[string]bool{"db": false, "tunnel": false}
		},
	}})

	for _, name := range []string{"db", "exec", "query", "seed", "topics", "trace", "tunnel"} {
		assertCommandHidden(t, root, name, true)
	}
	assertCommandHidden(t, root, "up", false)
}

func TestContextualHelpShowsOnlyConfiguredCapabilities(t *testing.T) {
	root := commandVisibilityTestRoot()
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {
				Ports: map[string]config.PortDef{"redis": {Host: 6379, Target: 6379}},
				Seed:  &config.SeedConfig{Files: []string{"users.json"}},
			},
		},
	}
	applyContextualCommandVisibility(root, cfg, []extension.Extension{{
		CommandVisibility: func(*config.Config) map[string]bool {
			return map[string]bool{"db": false, "tunnel": true}
		},
	}})

	for _, name := range []string{"exec", "query", "seed", "tunnel"} {
		assertCommandHidden(t, root, name, false)
	}
	for _, name := range []string{"db", "topics", "trace"} {
		assertCommandHidden(t, root, name, true)
	}

	cfg.Tracing = &config.TracingConfig{Enabled: true}
	applyContextualCommandVisibility(root, cfg, nil)
	assertCommandHidden(t, root, "trace", false)
}

func commandVisibilityTestRoot() *cobra.Command {
	root := &cobra.Command{Use: "orbit"}
	for _, name := range []string{"db", "exec", "query", "seed", "topics", "trace", "tunnel", "up"} {
		root.AddCommand(&cobra.Command{Use: name})
	}
	return root
}

func assertCommandHidden(t *testing.T, root *cobra.Command, name string, want bool) {
	t.Helper()
	cmd, _, err := root.Find([]string{name})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Hidden != want {
		t.Errorf("%s hidden = %v, want %v", name, cmd.Hidden, want)
	}
}
