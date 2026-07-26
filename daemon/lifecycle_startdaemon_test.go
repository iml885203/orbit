package daemon

import (
	"errors"
	"strconv"
	"testing"
)

// StartDaemon must refuse to fork when the dashboard port is already
// held. Otherwise a stale daemon keeps the TCP port and the new one
// silently starts without a dashboard (the exact behaviour that masked
// the "two daemons competing" bug on the user's machine).

func TestStartDaemon_FailsWhenDashboardPortHeld(t *testing.T) {
	holder, port := occupyPort(t)
	defer func() { _ = holder.Close() }()

	t.Setenv("ORBIT_DASHBOARD_PORT", strconv.Itoa(port))

	_, err := StartDaemon("/tmp/does-not-matter.yaml", nil)
	if err == nil {
		t.Fatal("expected StartDaemon to fail when dashboard port is held, got nil")
	}

	var conflict *PortConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected wrapped PortConflictError, got %T: %v", err, err)
	}
	if conflict.Port != port {
		t.Errorf("conflict.Port = %d, want %d", conflict.Port, port)
	}
}
