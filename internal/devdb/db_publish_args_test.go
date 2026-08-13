package devdb

import (
	"strings"
	"testing"
)

// `--parallel` carries an optional value, so it needs `=`. Writing
// `--parallel 4` leaves the 4 as a positional argument, and reporting only
// "--all takes no database argument" describes a mistake the user did not
// make. Reported from a live run where it cost a round of guessing.
func TestPublishArgsExplainsSpacedParallel(t *testing.T) {
	cmd := dbPublishCmd()
	t.Cleanup(func() { publishAll = false })

	for _, tc := range []struct {
		name     string
		all      bool
		args     []string
		wantErr  bool
		wantHint string
	}{
		{
			name:     "numeric positional suggests the = form",
			all:      true,
			args:     []string{"4"},
			wantErr:  true,
			wantHint: "--parallel=4",
		},
		{
			name:    "database name with --all stays the plain message",
			all:     true,
			args:    []string{"PlatformDB"},
			wantErr: true,
		},
		{
			name:    "--all alone is fine",
			all:     true,
			args:    nil,
			wantErr: false,
		},
		{
			name:    "one database without --all is fine",
			all:     false,
			args:    []string{"PlatformDB"},
			wantErr: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			publishAll = tc.all
			err := cmd.Args(cmd, tc.args)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, want error = %v", err, tc.wantErr)
			}
			if tc.wantHint == "" {
				if err != nil && strings.Contains(err.Error(), "--parallel=") {
					t.Errorf("unexpected parallel hint in %q", err)
				}
				return
			}
			if !strings.Contains(err.Error(), tc.wantHint) {
				t.Errorf("err = %q, want it to mention %q", err, tc.wantHint)
			}
		})
	}
}
