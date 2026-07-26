package port

import (
	"fmt"
	"net"
	"strconv"

	"github.com/iml885203/orbit/platform"
)

// Conflict represents a port already in use.
type Conflict struct {
	Port    int
	Service string // orbit service name that needs this port
	PID     string // PID of the process currently using the port
	Process string // name of the process currently using the port
}

// CheckPorts verifies that required ports are available.
// Returns a list of conflicts if any ports are in use.
func CheckPorts(portMap map[string][]int) []Conflict {
	var conflicts []Conflict

	for service, ports := range portMap {
		for _, port := range ports {
			if pid, proc, inUse := isPortInUse(port); inUse {
				conflicts = append(conflicts, Conflict{
					Port:    port,
					Service: service,
					PID:     pid,
					Process: proc,
				})
			}
		}
	}

	return conflicts
}

func isPortInUse(port int) (pid string, process string, inUse bool) {
	// Try to bind — fastest check
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err == nil {
		_ = ln.Close()
		return "", "", false
	}

	// Port is in use — find who's using it
	pid, process = findPortOwner(port)
	return pid, process, true
}

func findPortOwner(port int) (pid string, process string) {
	return platform.FindPortOwner(port)
}

// FindFree returns an OS-assigned free TCP port (binds :0, reads the port, closes).
func FindFree() (int, error) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("finding free port: %w", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// KillPortOwner attempts to kill the process using a given port.
func KillPortOwner(port int) error {
	pid, _, inUse := isPortInUse(port)
	if !inUse {
		return nil
	}
	if pid == "?" {
		return fmt.Errorf("cannot determine PID for port %d", port)
	}

	return platform.KillProcess(pid)
}
