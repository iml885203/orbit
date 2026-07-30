package port

import (
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"sort"
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

// ConflictError carries a port collision without exposing the runtime or
// container engine's raw bind error.
type ConflictError struct {
	Conflict
	InspectCommand string
}

func (e *ConflictError) Error() string {
	owner := ""
	if e.PID != "" && e.PID != "?" {
		owner = " by pid " + e.PID
		if e.Process != "" && e.Process != "?" {
			owner = " by " + filepath.Base(e.Process) + " (pid " + e.PID + ")"
		}
	}
	return fmt.Sprintf("cannot start %s: port %d is already in use%s", e.Service, e.Port, owner)
}

func NewConflictError(conflict Conflict) *ConflictError {
	return &ConflictError{
		Conflict:       conflict,
		InspectCommand: inspectCommand(conflict),
	}
}

// CheckPorts verifies that required ports are available.
// Returns a list of conflicts if any ports are in use.
func CheckPorts(portMap map[string][]int) []Conflict {
	var conflicts []Conflict
	services := make([]string, 0, len(portMap))
	for service := range portMap {
		services = append(services, service)
	}
	sort.Strings(services)
	for _, service := range services {
		ports := append([]int(nil), portMap[service]...)
		sort.Ints(ports)
		for _, portNumber := range ports {
			if pid, proc, inUse := isPortInUse(portNumber); inUse {
				conflicts = append(conflicts, Conflict{
					Port:    portNumber,
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
	addresses := []struct {
		network string
		address string
	}{
		{network: "tcp4", address: "127.0.0.1:" + strconv.Itoa(port)},
		{network: "tcp4", address: "0.0.0.0:" + strconv.Itoa(port)},
	}
	if probe, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		_ = probe.Close()
		addresses = append(addresses,
			struct {
				network string
				address string
			}{network: "tcp6", address: "[::1]:" + strconv.Itoa(port)},
			struct {
				network string
				address string
			}{network: "tcp6", address: "[::]:" + strconv.Itoa(port)},
		)
	}
	for _, address := range addresses {
		listener, err := net.Listen(address.network, address.address)
		if err != nil {
			pid, process = findPortOwner(port)
			return pid, process, true
		}
		_ = listener.Close()
	}
	return "", "", false
}

func findPortOwner(port int) (pid string, process string) {
	return platform.FindPortOwner(port)
}

func inspectCommand(conflict Conflict) string {
	if conflict.PID != "" && conflict.PID != "?" {
		if runtime.GOOS == "windows" {
			return "Get-Process -Id " + conflict.PID
		}
		return "ps -p " + conflict.PID + " -o pid,comm,args="
	}
	switch runtime.GOOS {
	case "darwin":
		return fmt.Sprintf("lsof -nP -iTCP:%d -sTCP:LISTEN", conflict.Port)
	case "windows":
		return fmt.Sprintf("Get-NetTCPConnection -LocalPort %d -State Listen", conflict.Port)
	default:
		return fmt.Sprintf("ss -ltnp 'sport = :%d'", conflict.Port)
	}
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
