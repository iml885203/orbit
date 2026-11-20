package process

import (
	"context"
	"errors"
	"fmt"
	"github.com/iml885203/orbit/internal/platform"
	"sync"
	"time"
)

type execution struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	grace    time.Duration
	graceSet bool
	err      error
	stopErr  error
}

func (m *Manager) ownershipChanged(name string) {
	if m.OnOwnershipChanged != nil {
		m.OnOwnershipChanged(name)
	}
}

func (m *Manager) finishExecution(name string, owner *execution, err error) {
	owner.cancel()
	owner.mu.Lock()
	m.mu.Lock()
	if m.executions[name] == owner && owner.stopErr == nil {
		delete(m.executions, name)
	}
	owner.err = err
	m.mu.Unlock()
	owner.mu.Unlock()
	m.ownershipChanged(name)
	close(owner.done)
}

type OwnershipSnapshot struct {
	PID     int
	PGID    int
	Stage   string
	Command string
	Dir     string
}

func (m *Manager) Ownership(name string) (OwnershipSnapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mp := m.processes[name]
	if mp == nil {
		return OwnershipSnapshot{}, false
	}
	pid := mp.ReconnectedPID
	if mp.Cmd != nil && mp.Cmd.Process != nil {
		pid = mp.Cmd.Process.Pid
	}
	if pid <= 0 || mp.PGID <= 0 {
		return OwnershipSnapshot{}, false
	}
	return OwnershipSnapshot{PID: pid, PGID: mp.PGID, Stage: mp.Stage, Command: mp.Command, Dir: mp.Dir}, true
}

func (m *Manager) stopExecution(name string, owner *execution, grace time.Duration) error {
	owner.mu.Lock()
	owner.grace, owner.graceSet = grace, true
	owner.cancel()
	owner.mu.Unlock()
	m.mu.RLock()
	current := m.executions[name] == owner
	child := m.processes[name]
	m.mu.RUnlock()
	if !current {
		return nil
	}
	if child != nil {
		if err := child.requestGroupTermination(grace); err != nil {
			return err
		}
	}
	select {
	case <-owner.done:
		owner.mu.Lock()
		failed := owner.stopErr
		result := owner.err
		owner.mu.Unlock()
		if failed != nil {
			retryErr := m.completeRetirement(name, owner, child)
			if errors.Is(result, ErrOutputDrainTimeout) {
				return errors.Join(retryErr, result)
			}
			return retryErr
		}
		if errors.Is(result, ErrOutputDrainTimeout) {
			return result
		}
		return nil
	case <-time.After(grace + 3*time.Second):
		return fmt.Errorf("process %q did not retire after stop", name)
	}
}

func (m *Manager) completeRetirement(name string, owner *execution, child *ManagedProcess) error {
	owner.mu.Lock()
	m.mu.Lock()
	if m.executions[name] != owner {
		m.mu.Unlock()
		owner.mu.Unlock()
		return nil
	}
	if child == nil || m.processes[name] != child {
		m.mu.Unlock()
		owner.mu.Unlock()
		return fmt.Errorf("process %q lost its failed retirement identity", name)
	}
	platform.CleanupJobObject(child.PGID)
	delete(m.executions, name)
	delete(m.processes, name)
	m.mu.Unlock()
	owner.stopErr = nil
	owner.mu.Unlock()
	m.ownershipChanged(name)
	return nil
}
