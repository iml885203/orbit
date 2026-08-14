package devdb

import (
	"strings"
	"testing"
)

// `--dacpac-dir ''` used to read as "flag omitted" and fall back to building
// from source, which then failed on project paths that do not exist on that
// machine — an error pointing at the config rather than at the argument.
// Reported from CI that signals "no artifacts this run" by clearing the
// variable it interpolates into the flag.
func TestInvocationDacpacDirRejectsAnEmptyArgument(t *testing.T) {
	t.Cleanup(func() { dacpacDir, dacpacDirGiven = "", false })

	for _, tc := range []struct {
		name    string
		value   string
		given   bool
		wantErr bool
	}{
		{name: "flag omitted", value: "", given: false},
		{name: "flag given an empty path", value: "", given: true, wantErr: true},
		{name: "flag given whitespace", value: "   ", given: true, wantErr: true},
		{name: "flag given a path", value: ".artifacts", given: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dacpacDir, dacpacDirGiven = tc.value, tc.given
			got, err := invocationDacpacDir()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("invocationDacpacDir() = %q, want an error", got)
				}
				if !strings.Contains(err.Error(), "omit the flag") {
					t.Errorf("err = %q, want it to say how to build from source instead", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("invocationDacpacDir() error = %v", err)
			}
			if tc.value == "" && got != "" {
				t.Errorf("omitted flag resolved to %q, want empty", got)
			}
			if tc.value != "" && got == "" {
				t.Errorf("a real path resolved to empty")
			}
		})
	}
}
