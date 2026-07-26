package daemon

import (
	"fmt"
	"net"

	"github.com/iml885203/orbit/process"
)

// PortConflictError describes why the dashboard TCP port could not be
// bound. It carries the offending PID so the caller can tell the user
// which process to kill.
type PortConflictError struct {
	Port int
	PID  int
	Err  error `tstype:"string"`
}

func (e *PortConflictError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("dashboard port %d already in use (held by pid %d)", e.Port, e.PID)
	}
	return fmt.Sprintf("dashboard port %d already in use: %v", e.Port, e.Err)
}

func (e *PortConflictError) Unwrap() error { return e.Err }

// ListenDashboard binds the dashboard TCP port, wrapping conflicts in
// PortConflictError so callers can render the who-holds-it hint.
// Callers should close the returned listener when done probing.
func ListenDashboard(port int) (net.Listener, error) {
	addr := fmt.Sprintf("localhost:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		pid := 0
		if holders := process.FindPortHolders([]int{port}); len(holders) > 0 {
			pid = holders[0].PID
		}
		return nil, &PortConflictError{Port: port, PID: pid, Err: err}
	}
	return ln, nil
}
