package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
)

func TestProjectContextNameUsesProjectDirectory(t *testing.T) {
	path := filepath.Join(string(filepath.Separator), "workspace", "shop", projectConfigName)
	if got := projectContextName(path); got != "shop" {
		t.Fatalf("name = %q", got)
	}
}

func TestProjectContextInactiveOffersOneSafeAction(t *testing.T) {
	err := projectContextInactive(
		filepath.Join("/workspace", "shop-b", projectConfigName),
		filepath.Join("/workspace", "shop-a", projectConfigName),
	)
	var typed projectContextInactiveError
	if !errors.As(err, &typed) {
		t.Fatalf("error type = %T", err)
	}
	if !strings.Contains(err.Error(), "shop-b is not running") ||
		!strings.Contains(err.Error(), "shop-a is still active") {
		t.Fatalf("error = %v", err)
	}
	withActions, ok := err.(interface{ CLIJSONActions() []cli.JSONAction })
	if !ok {
		t.Fatalf("error does not expose JSON actions: %T", err)
	}
	actions := withActions.CLIJSONActions()
	if len(actions) != 1 || actions[0].Command != "orbit up --json" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestProjectContextSwitchLabelsDisambiguateSameNamedProjects(t *testing.T) {
	switched := &projectContextSwitch{
		FromName: "payments", FromPath: "/workspace/a/payments/orbit.yaml",
		ToName: "payments", ToPath: "/workspace/b/payments/orbit.yaml",
	}
	from, to := projectContextSwitchLabels(switched)
	if from == to || !strings.Contains(from, switched.FromPath) || !strings.Contains(to, switched.ToPath) {
		t.Fatalf("labels = %q -> %q", from, to)
	}
}
