package process

import (
	"testing"
)

func TestFindPortHolders_UnusedPorts(t *testing.T) {
	// Ports in the high ephemeral range are very unlikely to be in use.
	holders := FindPortHolders([]int{59123, 59124, 59125})
	if len(holders) != 0 {
		t.Errorf("expected no holders for unused ports, got %d: %v", len(holders), holders)
	}
}

func TestKillGroup_InvalidPGID(t *testing.T) {
	tests := []struct {
		name string
		pgid int
	}{
		{"zero", 0},
		{"negative", -1},
		{"very negative", -9999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := KillGroup(tt.pgid, 0)
			if err == nil {
				t.Errorf("KillGroup(%d) should return error, got nil", tt.pgid)
			}
		})
	}
}

func TestKillGroup_NonExistentPGID(t *testing.T) {
	// A very large PGID that almost certainly does not exist.
	// KillGroup sends SIGTERM to -pgid; if the group doesn't exist the
	// syscall returns ESRCH and KillGroup treats that as success (already dead).
	err := KillGroup(999999999, 0)
	if err != nil {
		t.Errorf("KillGroup with non-existent pgid should return nil (ESRCH), got: %v", err)
	}
}

func TestManager_Stop_NonExistent(t *testing.T) {
	m := NewManager()

	// Stopping a non-existent process should be a no-op (returns nil).
	err := m.Stop("ghost", 0)
	if err != nil {
		t.Errorf("Stop on non-existent process should return nil, got: %v", err)
	}
}
