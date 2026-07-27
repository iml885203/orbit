package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/iml885203/orbit/process"
)

// The dashboard TCP port must fail loudly, not silently warn. A stale or
// foreign orbit still holding the port is the entire reason `daemon start`
// used to appear "succeed" while the new daemon quietly had no dashboard.

func TestListenDashboard_SucceedsOnFreePort(t *testing.T) {
	port := freePort(t)
	ln, err := ListenDashboard(port)
	if err != nil {
		t.Fatalf("ListenDashboard on free port: %v", err)
	}
	_ = ln.Close()
}

func TestListenDashboard_FailsWhenPortHeld(t *testing.T) {
	holder, port := occupyPort(t)
	defer func() { _ = holder.Close() }()

	ln, err := ListenDashboard(port)
	if err == nil {
		_ = ln.Close()
		t.Fatal("expected error when port is held, got nil")
	}
	var conflict *PortConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected PortConflictError, got %T: %v", err, err)
	}
	if conflict.Port != port {
		t.Errorf("conflict.Port = %d, want %d", conflict.Port, port)
	}
	if conflict.SuggestedPort <= 0 || conflict.SuggestedPort == port {
		t.Errorf("conflict.SuggestedPort = %d, want a different usable port", conflict.SuggestedPort)
	}
	suggested, suggestedErr := net.Listen("tcp", fmt.Sprintf("localhost:%d", conflict.SuggestedPort))
	if suggestedErr != nil {
		t.Fatalf("suggested port %d is not available: %v", conflict.SuggestedPort, suggestedErr)
	}
	_ = suggested.Close()
	if msg := conflict.Error(); !strings.Contains(msg, "already in use") {
		t.Errorf("error text should hint at port-in-use, got %q", msg)
	}
}

// PID lookup is best-effort — some environments (minimal CI containers
// without `ss`/`lsof`, or sandboxed sandboxes where `ss` can't see process
// owners) can't recover it. This test probes that capability first and
// skips when unavailable, so we still assert PID reporting on dev machines
// where regressions matter.
func TestListenDashboard_ReportsHolderPID(t *testing.T) {
	holder, port := occupyPort(t)
	defer func() { _ = holder.Close() }()

	if len(process.FindPortHolders([]int{port})) == 0 {
		t.Skip("PID lookup unavailable in this environment (no ss/lsof or insufficient privileges)")
	}

	ln, err := ListenDashboard(port)
	if err == nil {
		_ = ln.Close()
		t.Fatal("expected error when port is held, got nil")
	}
	var conflict *PortConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected PortConflictError, got %T: %v", err, err)
	}
	if conflict.PID != os.Getpid() {
		t.Errorf("conflict.PID = %d, want %d (this test process)", conflict.PID, os.Getpid())
	}
}

// freePort binds :0, records the port, closes the listener. There is a
// tiny race window before a caller binds it again; acceptable for tests.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// occupyPort grabs an ephemeral port and returns the listener + port. The
// caller must Close() the listener when done.
func occupyPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy port: %v", err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}
