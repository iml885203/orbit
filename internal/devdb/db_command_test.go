package devdb

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestDBCommand_ExposesOnlyUserWorkflow(t *testing.T) {
	cmd := DBCmd()
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
			t.Errorf("%s must be rejected instead of falling back to db help", removed)
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
