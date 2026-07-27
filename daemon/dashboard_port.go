package daemon

import (
	"fmt"
	"net"

	"github.com/iml885203/orbit/process"
)

// PortConflictError describes why the dashboard TCP port could not be
// bound. It carries owner evidence and a verified free alternative so the
// caller can offer recovery without asking the user to investigate ports.
type PortConflictError struct {
	Port          int
	PID           int
	SuggestedPort int
	Err           error `tstype:"string"`
}

func (e *PortConflictError) Error() string {
	if e.PID > 0 {
		if e.SuggestedPort <= 0 {
			return fmt.Sprintf("dashboard port %d already in use (held by pid %d)", e.Port, e.PID)
		}
		return fmt.Sprintf("dashboard port %d already in use (held by pid %d); port %d is available", e.Port, e.PID, e.SuggestedPort)
	}
	if e.SuggestedPort <= 0 {
		return fmt.Sprintf("dashboard port %d already in use: %v", e.Port, e.Err)
	}
	return fmt.Sprintf("dashboard port %d already in use; port %d is available: %v", e.Port, e.SuggestedPort, e.Err)
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
		return nil, &PortConflictError{
			Port:          port,
			PID:           pid,
			SuggestedPort: availableDashboardPort(port),
			Err:           err,
		}
	}
	return ln, nil
}

func availableDashboardPort(after int) int {
	for candidate := after + 1; candidate <= after+100 && candidate <= 65535; candidate++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", candidate))
		if err != nil {
			continue
		}
		_ = listener.Close()
		return candidate
	}
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}
