package app

import (
	"errors"
	"testing"

	"github.com/iml885203/orbit/cli"
)

func TestValidateUpSelectionRejectsIgnoredSelectors(t *testing.T) {
	originalInfraOnly, originalGroups := infraOnly, groups
	t.Cleanup(func() {
		infraOnly = originalInfraOnly
		groups = originalGroups
	})

	tests := []struct {
		name   string
		args   []string
		infra  bool
		groups []string
	}{
		{name: "service with infra", args: []string{"api"}, infra: true},
		{name: "group with infra", infra: true, groups: []string{"web"}},
		{name: "service with group", args: []string{"api"}, groups: []string{"web"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			infraOnly, groups = tt.infra, tt.groups
			err := validateUpSelection(tt.args)
			if !errors.Is(err, cli.ErrInvalidArgument) {
				t.Fatalf("error = %v, want invalid argument", err)
			}
		})
	}
}

func TestValidateUpSelectionAcceptsOneSelector(t *testing.T) {
	originalInfraOnly, originalGroups := infraOnly, groups
	t.Cleanup(func() {
		infraOnly = originalInfraOnly
		groups = originalGroups
	})

	for _, tt := range []struct {
		args   []string
		infra  bool
		groups []string
	}{
		{},
		{args: []string{"api"}},
		{infra: true},
		{groups: []string{"web"}},
	} {
		infraOnly, groups = tt.infra, tt.groups
		if err := validateUpSelection(tt.args); err != nil {
			t.Fatalf("selection %+v failed: %v", tt, err)
		}
	}
}
