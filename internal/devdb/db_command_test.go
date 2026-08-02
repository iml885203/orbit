package devdb

import (
	"errors"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/spf13/cobra"
)

func TestSQLServerCommandExposesOnlyProviderWorkflow(t *testing.T) {
	cmd := SQLServerCmd()
	if !strings.Contains(cmd.Short, "SQL Server Database Projects") {
		t.Fatalf("sqlserver command does not identify its provider-specific workflow: %q", cmd.Short)
	}
	visible := map[string]bool{}
	for _, child := range cmd.Commands() {
		if !child.Hidden {
			visible[child.Name()] = true
		}
	}

	for _, name := range []string{"list", "query", "diff", "publish", "reset"} {
		if !visible[name] {
			t.Errorf("%s must be visible", name)
		}
		delete(visible, name)
	}
	if len(visible) != 0 {
		t.Errorf("unexpected public database commands: %v", visible)
	}
	for _, removed := range []string{"snapshot", "status", "bootstrap"} {
		if commandFound(cmd, removed) {
			t.Errorf("%s must not remain as a callable command", removed)
		}
		if err := cmd.RunE(cmd, []string{removed}); err == nil {
			t.Errorf("%s must be rejected instead of falling back to sqlserver help", removed)
		}
	}
}

func TestSQLServerNotConfiguredErrorNamesSourceConfigAndGuide(t *testing.T) {
	err := sqlServerNotConfiguredError{configPath: "/workspace/envs/local.yaml"}
	if !errors.Is(err, cli.ErrNotConfigured) {
		t.Fatalf("error does not classify as not configured: %v", err)
	}
	for _, evidence := range []string{
		`"/workspace/envs/local.yaml"`,
		"sqlserver.target",
		"sqlserver.projects",
		sqlServerConfigurationGuide,
	} {
		if !strings.Contains(err.Error(), evidence) {
			t.Errorf("error missing %q: %v", evidence, err)
		}
	}
}

func commandFound(cmd *cobra.Command, name string) bool {
	for _, child := range cmd.Commands() {
		if child.Name() == name {
			return true
		}
	}
	return false
}
