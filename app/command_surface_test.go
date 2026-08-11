package app

import (
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/extension"
	"github.com/spf13/cobra"
)

func TestUserDirectedCommandsAreDiscoverable(t *testing.T) {
	commands := map[string]*cobra.Command{
		"daemon":   daemonCmd(),
		"settings": settingsCmd(),
		"service":  serviceCmd(),
		"tracing":  tracingCmd(),
	}
	for name, cmd := range commands {
		if cmd.Hidden {
			t.Errorf("%s must be visible because Orbit directs users to it", name)
		}
	}
	apply, _, err := envCmd().Find([]string{"apply"})
	if err != nil {
		t.Fatal(err)
	}
	if apply.Hidden {
		t.Error("env apply must be visible because lifecycle errors direct users to it")
	}
	if !inspectCmd().Hidden {
		t.Error("inspect stays out of the human command list and is signposted separately")
	}
	if !strings.Contains(rootCommandDescription, "orbit inspect --json") ||
		!strings.Contains(rootCommandDescription, "https://iml885203.github.io/orbit/docs/agent-cli") {
		t.Fatalf("root help does not lead agents to the JSON entry point: %q", rootCommandDescription)
	}
}

func TestLifecycleHelpExplainsSelectionAsymmetry(t *testing.T) {
	for _, want := range []string{"transitive dependencies", "all configured containers"} {
		if !strings.Contains(upCmd().Long, want) {
			t.Errorf("up help missing %q", want)
		}
	}
	for _, want := range []string{"does not expand to dependencies", "remain running"} {
		if !strings.Contains(downCmd().Long, want) {
			t.Errorf("down help missing %q", want)
		}
	}
}

func TestDiagnosticHelpSeparatesConfigurationFromRuntimeReadiness(t *testing.T) {
	checks := []struct {
		name string
		text string
		want string
	}{
		{"status", statusCmd().Long, "orbit inspect --json"},
		{"doctor", doctorCmd().Long, "host runtimes"},
		{"env info", envInfoCmd().Long, "describes configuration"},
		{"inspect", inspectCmd().Short, "runtime readiness"},
	}
	for _, check := range checks {
		if !strings.Contains(check.text, check.want) {
			t.Errorf("%s help missing %q: %q", check.name, check.want, check.text)
		}
	}
	if got := envInfoCmd().Use; got != "info [environment]" {
		t.Errorf("env info use = %q", got)
	}
}

func TestDestructiveHelpNamesDataRemoval(t *testing.T) {
	clean, _, err := instanceCmd().Find([]string{"clean"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"volumes", "Data", "lost"} {
		if !strings.Contains(clean.Long, want) {
			t.Errorf("instance clean help missing %q: %q", want, clean.Long)
		}
	}
	if !strings.Contains(uninstallCmd().Long, "--purge") {
		t.Error("uninstall help must distinguish the purge path")
	}
}

func TestRuntimeCommandsRequireMatchingDaemonConfig(t *testing.T) {
	root := &cobra.Command{Use: "orbit"}
	for _, name := range []string{
		"down", "restart", "logs", "open", "exec", "query", "topics", "seed",
		"edge", "service", "sqlserver", "tunnel", "trace", "tracing",
	} {
		parent := &cobra.Command{Use: name}
		child := &cobra.Command{Use: "child"}
		root.AddCommand(parent)
		parent.AddCommand(child)
		if !commandRequiresMatchingDaemonConfig(child) {
			t.Errorf("nested %s command did not require matching daemon config", name)
		}
	}
	for _, name := range []string{"up", "status", "doctor", "inspect", "history", "env", "daemon"} {
		cmd := &cobra.Command{Use: name}
		root.AddCommand(cmd)
		if commandRequiresMatchingDaemonConfig(cmd) {
			t.Errorf("%s should remain available across project contexts", name)
		}
	}
}

func TestMutationsRequireReconciledDaemon(t *testing.T) {
	root := &cobra.Command{Use: "orbit"}
	up := &cobra.Command{Use: "up"}
	restart := &cobra.Command{Use: "restart"}
	status := &cobra.Command{Use: "status"}
	down := &cobra.Command{Use: "down"}
	env := &cobra.Command{Use: "env"}
	toggle := &cobra.Command{Use: "toggle"}
	apply := &cobra.Command{Use: "apply"}
	list := &cobra.Command{Use: "list"}
	root.AddCommand(up, restart, status, down, env)
	env.AddCommand(toggle, apply, list)

	for _, cmd := range []*cobra.Command{restart, toggle} {
		if !commandRequiresReconciledDaemon(cmd) {
			t.Errorf("%s did not require a reconciled daemon", cmd.CommandPath())
		}
	}
	for _, cmd := range []*cobra.Command{up, status, down, apply, list} {
		if commandRequiresReconciledDaemon(cmd) {
			t.Errorf("%s should remain available or converge during reconciliation", cmd.CommandPath())
		}
	}
}

func TestUnavailableEnvironmentBlocksMutationsButKeepsRecoveryAvailable(t *testing.T) {
	root := &cobra.Command{Use: "orbit"}
	up := &cobra.Command{Use: "up"}
	status := &cobra.Command{Use: "status"}
	down := &cobra.Command{Use: "down"}
	switchEnv := &cobra.Command{Use: "switch"}
	env := &cobra.Command{Use: "env"}
	list := &cobra.Command{Use: "list"}
	toggle := &cobra.Command{Use: "toggle"}
	apply := &cobra.Command{Use: "apply"}
	daemonCmd := &cobra.Command{Use: "daemon"}
	daemonStart := &cobra.Command{Use: "start"}
	daemonStop := &cobra.Command{Use: "stop"}
	root.AddCommand(up, status, down, switchEnv, env, daemonCmd)
	env.AddCommand(list, toggle, apply)
	daemonCmd.AddCommand(daemonStart, daemonStop)

	for _, cmd := range []*cobra.Command{up, toggle, apply, daemonStart} {
		if !commandRequiresAvailableEnvironment(cmd) {
			t.Errorf("%s did not require an available environment", cmd.CommandPath())
		}
	}
	for _, cmd := range []*cobra.Command{status, down, switchEnv, list, daemonStop} {
		if commandRequiresAvailableEnvironment(cmd) {
			t.Errorf("%s should remain available for recovery", cmd.CommandPath())
		}
	}
}

func TestCommandTreeQueriesStayUsableBeforeSetup(t *testing.T) {
	root := &cobra.Command{Use: "orbit"}
	up := &cobra.Command{Use: "up"}
	help := &cobra.Command{Use: "help"}
	completion := &cobra.Command{Use: "completion"}
	zsh := &cobra.Command{Use: "zsh"}
	root.AddCommand(up, help, completion)
	completion.AddCommand(zsh)

	for _, cmd := range []*cobra.Command{help, completion, zsh} {
		if !isCommandTreeQuery(cmd) {
			t.Errorf("%s must stay usable before setup", cmd.CommandPath())
		}
	}
	if isCommandTreeQuery(up) {
		t.Error("up is not a command-tree query and must keep its setup gate")
	}
}

func TestContextualHelpHidesCommandsWithoutASelectedEnvironment(t *testing.T) {
	root := commandVisibilityTestRoot()
	applyContextualCommandVisibility(root, nil, []extension.Extension{{
		CommandVisibility: func(*config.Config) map[string]bool {
			return map[string]bool{"sqlserver": false, "tunnel": false}
		},
	}})

	for _, name := range []string{"sqlserver", "exec", "query", "seed", "topics", "trace", "tunnel"} {
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
				Seed:  &config.SeedConfig{Command: "mongosh --quiet app", Files: []string{"users.json"}},
			},
		},
	}
	applyContextualCommandVisibility(root, cfg, []extension.Extension{{
		CommandVisibility: func(*config.Config) map[string]bool {
			return map[string]bool{"sqlserver": true, "tunnel": true}
		},
	}})

	for _, name := range []string{"sqlserver", "exec", "query", "seed", "trace", "tunnel"} {
		assertCommandHidden(t, root, name, false)
	}
	for _, name := range []string{"topics"} {
		assertCommandHidden(t, root, name, true)
	}

	disabled := false
	cfg.Tracing = &config.TracingConfig{Enabled: &disabled}
	applyContextualCommandVisibility(root, cfg, nil)
	assertCommandHidden(t, root, "trace", true)
}

func TestContextualHelpShowsQueryForPostgresOnlyEnvironment(t *testing.T) {
	root := commandVisibilityTestRoot()
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"database": {
				Ports: map[string]config.PortDef{"postgres": {Host: 5432, Target: 5432}},
			},
		},
	}

	applyContextualCommandVisibility(root, cfg, nil)

	assertCommandHidden(t, root, "query", false)
}

func commandVisibilityTestRoot() *cobra.Command {
	root := &cobra.Command{Use: "orbit"}
	for _, name := range []string{"sqlserver", "exec", "query", "seed", "topics", "trace", "tunnel", "up"} {
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
