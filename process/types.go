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
	stopOnce  sync.Once     // the lifecycle cancel and explicit stop share one termination
	stopErr   error
	mu        sync.Mutex
}

// CloseDone safely closes the Done channel exactly once.
func (mp *ManagedProcess) CloseDone() {
	mp.closeOnce.Do(func() { close(mp.Done) })
}

// stopGroup lets cancellation and explicit lifecycle commands converge on
// one process-group termination. Without this coordination, a restart can
// send two signals concurrently and misclassify the resulting race as a
// failed stop or a port conflict.
func (mp *ManagedProcess) stopGroup(gracePeriod time.Duration) error {
	mp.stopOnce.Do(func() {
		mp.stopErr = KillGroup(mp.PGID, gracePeriod)
	})
	return mp.stopErr
}
