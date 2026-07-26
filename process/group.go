package process

import (
	"fmt"
	"syscall"
	"time"

	"github.com/iml885203/orbit/platform"
)

// KillGroup sends SIGTERM to the entire process group, waits for graceful
// shutdown, then sends SIGKILL if still alive. This is the core of the
// zero-zombie guarantee.
func KillGroup(pgid int, gracePeriod time.Duration) error {
	if pgid <= 0 {
		return fmt.Errorf("invalid pgid: %d", pgid)
	}

	// Send SIGTERM to the entire process group
	if err := platform.KillProcessGroup(pgid, syscall.SIGTERM); err != nil {
		if !platform.IsGroupAlive(pgid) {
			return nil // already dead
		}
		return fmt.Errorf("SIGTERM to group %d: %w", pgid, err)
	}

	// Wait for graceful shutdown
	deadline := time.After(gracePeriod)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			// Grace period expired — force kill
			if err := platform.KillProcessGroup(pgid, syscall.SIGKILL); err != nil {
				if !platform.IsGroupAlive(pgid) {
					return nil
				}
				return fmt.Errorf("SIGKILL to group %d: %w", pgid, err)
			}
			return nil
		case <-ticker.C:
			if !platform.IsGroupAlive(pgid) {
				return nil // all dead
			}
		}
	}
}

// PortHolder describes a process holding a port.
type PortHolder struct {
	Port int
	PID  int
}

// FindPortHolders checks which ports are still in use and returns the PIDs holding them.
func FindPortHolders(ports []int) []PortHolder {
	var holders []PortHolder
	for _, port := range ports {
		for _, pid := range platform.FindPortHolderPIDs(port) {
			holders = append(holders, PortHolder{Port: port, PID: pid})
		}
	}
	return holders
}
