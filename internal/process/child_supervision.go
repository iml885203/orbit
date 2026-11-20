package process

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"

	"github.com/iml885203/orbit/internal/logging"
	"github.com/iml885203/orbit/internal/platform"
)

func (m *Manager) monitorChild(ctx context.Context, owner *execution, mp *ManagedProcess, stdoutPipe, stderrPipe *os.File) func(bool) {
	decision := make(chan struct{})
	launchAccepted := false
	var decisionOnce sync.Once
	decide := func(accepted bool) { decisionOnce.Do(func() { launchAccepted = accepted; close(decision) }) }

	grace := m.CancelGrace
	if grace <= 0 {
		grace = defaultCancelGrace
	}
	go func() {
		select {
		case <-ctx.Done():
			owner.mu.Lock()
			selectedGrace := grace
			if owner.graceSet {
				selectedGrace = owner.grace
			}
			owner.mu.Unlock()
			_ = mp.observeGroupTermination(selectedGrace)
		case <-mp.Done:
		}
	}()

	// Keep stderr scoped to this process generation. The combined log remains
	// the user-facing stream; the separate buffer exists only so failure
	// summaries do not guess causality from ordinary stdout traffic.
	stderr := logging.NewRingBuffer(20)
	outputDone := make(chan struct{}, 2)
	go func() {
		defer func() { _ = stdoutPipe.Close(); outputDone <- struct{}{} }()
		m.streamOutput(mp.Name, stdoutPipe, nil)
	}()
	go func() {
		defer func() { _ = stderrPipe.Close(); outputDone <- struct{}{} }()
		m.streamOutput(mp.Name, stderrPipe, stderr.Write)
	}()

	go func() {
		waitErr := mp.Cmd.Wait()
		// Descendants may still flush output after their leader exits. Join the
		// existing group termination owner before starting the final EOF budget.
		cleanupErr := mp.observeGroupTermination(grace)
		drainErr := waitForProcessOutput(outputDone, stdoutPipe, stderrPipe)
		mp.mu.Lock()
		mp.Err = errors.Join(waitErr, drainErr, cleanupErr)
		mp.mu.Unlock()

		if mp.Err != nil {
			slog.Warn("exited", "component", "process", "name", mp.Name, "err", mp.Err)
		} else {
			slog.Info("exited", "component", "process", "name", mp.Name)
		}
		if mp.Err != nil {
			m.narrate(mp.Name, "exited: "+mp.Err.Error())
		} else {
			m.narrate(mp.Name, "exited")
		}
		if cleanupErr == nil {
			platform.CleanupJobObject(mp.Cmd.Process.Pid)
		}

		<-decision
		m.mu.Lock()
		if m.processes[mp.Name] == mp && cleanupErr == nil {
			delete(m.processes, mp.Name)
		}
		m.mu.Unlock()
		owner.mu.Lock()
		owner.stopErr = cleanupErr
		owner.mu.Unlock()
		m.ownershipChanged(mp.Name)
		mp.CloseDone()

		if mp.Stage == "main" && launchAccepted {
			m.finishExecution(mp.Name, owner, mp.Err)
			if m.OnExit != nil {
				m.OnExit(mp.Name, mp.Epoch, mp.Err, stderr.Lines())
			}
		}
	}()

	return decide
}
