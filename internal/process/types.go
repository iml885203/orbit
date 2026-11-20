package process

import (
	"os/exec"
	"sync"
	"time"
)

// ManagedProcess tracks a running child process and its process group.
type ManagedProcess struct {
	Stage          string
	Command        string
	Dir            string
	Name           string
	Cmd            *exec.Cmd // nil for reconnected processes
	PGID           int
	ReconnectedPID int // set for reconnected processes (Cmd is nil)
	// Epoch echoes Start's epoch argument so OnExit can attribute the exit
	// to the process generation that actually died. 0 for reconnected
	// processes, which predate any epoch.
	Epoch       int
	Started     time.Time
	Done        chan struct{} // closed when process exits
	Err         error         // exit error, if any
	closeOnce   sync.Once     // guards Done channel close
	stopAttempt *groupTermination
	terminate   func(time.Duration) error
	mu          sync.Mutex
}

// CloseDone safely closes the Done channel exactly once.
func (mp *ManagedProcess) CloseDone() {
	mp.closeOnce.Do(func() { close(mp.Done) })
}

type groupTermination struct {
	done chan struct{}
	err  error
}

func (mp *ManagedProcess) observeGroupTermination(grace time.Duration) error {
	return mp.groupTerminationAttempt(grace, false)
}

func (mp *ManagedProcess) requestGroupTermination(grace time.Duration) error {
	return mp.groupTerminationAttempt(grace, true)
}

func (mp *ManagedProcess) groupTerminationAttempt(gracePeriod time.Duration, retryFailure bool) error {
	mp.mu.Lock()
	previous := mp.stopAttempt
	if previous != nil {
		select {
		case <-previous.done:
			if previous.err == nil || !retryFailure {
				mp.mu.Unlock()
				return previous.err
			}
		default:
			mp.mu.Unlock()
			<-previous.done
			return previous.err
		}
	}
	attempt := &groupTermination{done: make(chan struct{})}
	mp.stopAttempt = attempt
	mp.mu.Unlock()
	if mp.terminate != nil {
		attempt.err = mp.terminate(gracePeriod)
	} else {
		attempt.err = KillGroup(mp.PGID, gracePeriod)
	}
	close(attempt.done)
	return attempt.err
}
