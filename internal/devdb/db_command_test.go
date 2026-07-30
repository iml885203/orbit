package devdb

import (
	"strings"
	"testing"

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

func TestDBMigrationCommandPointsToProviderSpecificName(t *testing.T) {
	cmd := DBMigrationCmd()
	if !cmd.Hidden {
		t.Fatal("migration guard must not appear in help")
	}
	err := cmd.RunE(cmd, []string{"diff", "Sample DB"})
	renamed, ok := err.(dbCommandRenamedError)
	if !ok {
		t.Fatalf("migration error = %T, want dbCommandRenamedError", err)
	}
	if got, want := renamed.CLIHumanNextCommand(), "orbit sqlserver diff 'Sample DB'"; got != want {
		t.Fatalf("next command = %q, want %q", got, want)
	}
	actions := renamed.CLIJSONReplacementActions()
	if len(actions) != 1 || actions[0].Command != renamed.CLIHumanNextCommand() {
		t.Fatalf("replacement actions = %+v", actions)
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
