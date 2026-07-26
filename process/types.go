package process

import (
	"os/exec"
	"sync"
	"time"
)

// ManagedProcess tracks a running child process and its process group.
type ManagedProcess struct {
	Name           string
	Cmd            *exec.Cmd // nil for reconnected processes
	PGID           int
	ReconnectedPID int // set for reconnected processes (Cmd is nil)
	// Epoch echoes Start's epoch argument so OnExit can attribute the exit
	// to the process generation that actually died. 0 for reconnected
	// processes, which predate any epoch.
	Epoch     int
	Started   time.Time
	Done      chan struct{} // closed when process exits
	Err       error         // exit error, if any
	closeOnce sync.Once     // guards Done channel close
	mu        sync.Mutex
}

// CloseDone safely closes the Done channel exactly once.
func (mp *ManagedProcess) CloseDone() {
	mp.closeOnce.Do(func() { close(mp.Done) })
}
