package app

import (
	"path/filepath"
	"strings"
	"testing"
)

// `daemon restart -c other.yaml` used to report success and restart the
// environment already running, because restart takes the running config over
// the resolved one. Reported from a live session where the user then found
// three different answers for one environment — `status`, `daemon status` and
// `instance list` — with no command that changed any of them.
func TestRejectRestartAcrossEnvironments(t *testing.T) {
	dir := t.TempDir()
	running := filepath.Join(dir, "backoffice.yaml")
	requested := filepath.Join(dir, "e2e.yaml")

	for _, tc := range []struct {
		name           string
		running        string
		requested      string
		explicitConfig bool
		wantErr        bool
	}{
		{
			name:    "explicit flag naming another environment is refused",
			running: running, requested: requested, explicitConfig: true,
			wantErr: true,
		},
		{
			name:    "explicit flag naming the running environment proceeds",
			running: running, requested: running, explicitConfig: true,
		},
		{
			name:    "a resolved default is not a request",
			running: running, requested: requested, explicitConfig: false,
		},
		{
			name:    "nothing running yet",
			running: "", requested: requested, explicitConfig: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := rejectRestartAcrossEnvironments(tc.running, tc.requested, tc.explicitConfig)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, want error = %v", err, tc.wantErr)
			}
			if !tc.wantErr {
				return
			}
			// The message has to carry the way out, not just the refusal:
			// there is no restart flag that switches environments.
			for _, want := range []string{tc.running, tc.requested, "orbit down"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %q, want it to mention %q", err, want)
				}
			}
		})
	}
}
