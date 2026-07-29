package app

import (
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/extension"
	"github.com/spf13/cobra"
)

func TestRuntimeCommandsRequireMatchingDaemonConfig(t *testing.T) {
	root := &cobra.Command{Use: "orbit"}
	for _, name := range []string{
		"down", "restart", "logs", "open", "exec", "query", "topics", "seed",
		"edge", "service", "db", "tunnel", "trace", "tracing",
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

	for _, cmd := range []*cobra.Command{up, restart, toggle} {
		if !commandRequiresReconciledDaemon(cmd) {
			t.Errorf("%s did not require a reconciled daemon", cmd.CommandPath())
		}
	}
	for _, cmd := range []*cobra.Command{status, down, apply, list} {
		if commandRequiresReconciledDaemon(cmd) {
			t.Errorf("%s should remain available during reconciliation", cmd.CommandPath())
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
