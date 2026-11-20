package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/iml885203/orbit/internal/logging"
	"github.com/iml885203/orbit/internal/platform"
)

// Manager manages child processes with process group isolation.
type Manager struct {
	processes      map[string]*ManagedProcess
	executions     map[string]*execution
	mu             sync.RWMutex
	terminateGroup func(int, time.Duration) error
	resumeProcess  func(int) error

	// CancelGrace is the SIGTERM→SIGKILL window used when the ctx passed
	// to Start is cancelled. Falls back to 5s if zero so shutdown handlers
	// still get a chance to run.
	CancelGrace time.Duration

	// OnOutput is called for each line of stdout/stderr from a process.
	OnOutput func(name string, line string)
	// OnExit is called when a process exits. stderr contains only output from
	// this process generation, so callers never mistake an earlier run or a
	// successful stdout request for crash evidence. epoch echoes the value
	// given to Start for the process that actually exited, so a late exit
	// from a replaced process can't be attributed to its successor (0 for
	// reconnected processes, which predate any epoch).
	OnExit             func(name string, epoch int, err error, stderr []string)
	OnStarted          func(name string)
	OnOwnershipChanged func(name string)
	// OnAction narrates lifecycle actions (start/stop/exit) for the dashboard.
	OnAction func(name string, msg string)
}

const defaultCancelGrace = 5 * time.Second

func (m *Manager) narrate(name, msg string) {
	if m.OnAction != nil {
		m.OnAction(name, msg)
	}
}

// emitLine writes a single synthetic log line for the given service through
// the same OnOutput path used by streamed process output. It is a no-op if
// OnOutput is unset (mirrors the nil-guard inside streamOutput).
func (m *Manager) emitLine(name, line string) {
	if m.OnOutput != nil {
		m.OnOutput(name, line)
	}
}

// NewManager creates a new process manager.
func NewManager() *Manager {
	return &Manager{
		processes:  make(map[string]*ManagedProcess),
		executions: make(map[string]*execution),
	}
}

// Start launches a process in its own process group. The process tree is
// bound to ctx: cancellation kills the whole group (not just the leader),
// which is what prevents zombies when a stop is issued mid-startup.
// Start launches the service process. epoch is an opaque caller token echoed
// back on OnExit — the engine passes the service generation so late exit
// notifications can't be attributed to a newer process.
func (m *Manager) Start(ctx context.Context, name, dir, command string, env map[string]string, preStart []string, epoch int) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start %q aborted: %w", name, err)
	}
	childContext, cancel := context.WithCancel(ctx)
	owner := &execution{cancel: cancel, done: make(chan struct{})}
	m.mu.Lock()
	if _, exists := m.processes[name]; exists {
		m.mu.Unlock()
		cancel()
		return fmt.Errorf("process %q already running", name)
	}
	if _, exists := m.executions[name]; exists {
		m.mu.Unlock()
		cancel()
		return fmt.Errorf("process %q already running", name)
	}
	m.executions[name] = owner
	m.mu.Unlock()
	launched := false
	defer func() {
		if !launched {
			m.finishExecution(name, owner, resultErr)
		}
	}()
	for _, pre := range preStart {
		if err := m.runPreStart(childContext, owner, name, dir, pre, env, epoch); err != nil {
			return err
		}
	}
	if _, err := m.launchChild(childContext, owner, name, dir, command, "main", env, epoch); err != nil {
		return err
	}
	launched = true
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start %q aborted: %w", name, err)
	}
	return nil
}

func (m *Manager) runPreStart(ctx context.Context, owner *execution, name, dir, pre string, env map[string]string, epoch int) error {
	parts, err := commandArgs(pre, env)
	if err != nil {
		return fmt.Errorf("pre_start for %q: %w", name, err)
	}
	if len(parts) == 0 {
		return ctx.Err()
	}
	m.narrate(name, fmt.Sprintf("running pre_start: %s", pre))
	m.emitLine(name, fmt.Sprintf("[pre_start] $ %s", pre))
	mp, err := m.launchChild(ctx, owner, name, dir, pre, "pre_start", env, epoch)
	if err != nil {
		m.emitLine(name, fmt.Sprintf("[pre_start] start error: %v", err))
		return fmt.Errorf("pre_start %q start: %w", pre, err)
	}
	<-mp.Done
	mp.mu.Lock()
	waitErr := mp.Err
	mp.mu.Unlock()
	if waitErr != nil || ctx.Err() != nil {
		waitErr = errors.Join(waitErr, ctx.Err())
		exitCode := -1
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			exitCode = ee.ExitCode()
		}
		m.emitLine(name, fmt.Sprintf("[pre_start] exit %d", exitCode))
		m.narrate(name, fmt.Sprintf("pre_start failed (exit=%d): %s", exitCode, pre))
		return fmt.Errorf("pre_start %q failed: %w", pre, waitErr)
	}
	m.emitLine(name, "[pre_start] exit 0")
	m.narrate(name, fmt.Sprintf("pre_start ok: %s", pre))
	return nil
}

func (m *Manager) launchChild(ctx context.Context, owner *execution, name, dir, command, stage string, env map[string]string, epoch int) (*ManagedProcess, error) {
	owner.mu.Lock()
	registrationLocked := true
	defer func() {
		if registrationLocked {
			owner.mu.Unlock()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parts, err := commandArgs(command, env)
	if err != nil {
		return nil, fmt.Errorf("command for %q: %w", name, err)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command for %q", name)
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	// The lifecycle watcher owns termination of the whole process group;
	// CommandContext's default leader kill would bypass its grace window.
	cmd.Cancel = nil
	cmd.Dir = dir

	cmd.Env = commandEnvironment(env)

	platform.SetProcessGroup(cmd)

	stdoutPipe, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe for %q: %w", name, err)
	}
	defer func() { _ = stdoutWriter.Close() }()
	stderrPipe, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdoutPipe.Close()
		return nil, fmt.Errorf("stderr pipe for %q: %w", name, err)
	}
	defer func() { _ = stderrWriter.Close() }()
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	m.narrate(name, fmt.Sprintf("starting process (cmd=%q, dir=%q)", command, dir))
	if err := cmd.Start(); err != nil {
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		m.narrate(name, "ERROR: "+err.Error())
		return nil, fmt.Errorf("starting %q (cmd=%q dir=%q): %w", name, command, dir, err)
	}
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	slog.Info("started", "component", "process", "name", name, "pid", cmd.Process.Pid, "cmd", command, "dir", dir)
	m.narrate(name, fmt.Sprintf("started (pid=%d)", cmd.Process.Pid))

	// Assign to Job Object (Windows) — ensures child process tree is killed on stop.
	// No-op on Unix where process groups handle this.
	if err := platform.AssignJobObject(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		return nil, fmt.Errorf("owning process tree for %q: %w", name, err)
	}

	pgid, err := platform.GetProcessGroup(cmd.Process.Pid)
	if err != nil {
		pgid = cmd.Process.Pid // fallback
	}

	mp := &ManagedProcess{
		Stage: stage, Command: command, Dir: dir,
		Name:    name,
		Cmd:     cmd,
		PGID:    pgid,
		Started: time.Now(),
		Done:    make(chan struct{}),
		Epoch:   epoch,
	}

	m.mu.Lock()
	mp.terminate = func(grace time.Duration) error {
		if m.terminateGroup != nil {
			return m.terminateGroup(mp.PGID, grace)
		}
		return KillGroup(mp.PGID, grace)
	}
	m.processes[name] = mp
	m.mu.Unlock()
	owner.mu.Unlock()
	registrationLocked = false
	m.ownershipChanged(name)
	decide := m.monitorChild(ctx, owner, mp, stdoutPipe, stderrPipe)
	defer decide(false)

	resume := m.resumeProcess
	if resume == nil {
		resume = platform.ResumeProcess
	}
	resumeErr := ctx.Err()
	if resumeErr == nil {
		resumeErr = resume(cmd.Process.Pid)
	}
	if resumeErr != nil {
		decide(false)
		owner.cancel()
		<-mp.Done
		return nil, resumeErr
	}
	if stage == "main" && m.OnStarted != nil {
		m.OnStarted(name)
	}
	decide(true)
	return mp, nil
}

func (m *Manager) streamOutput(name string, r io.Reader, tee func(string)) {
	lb := logging.NewLineBuffer(func(line string) {
		if tee != nil {
			tee(line)
		}
		if m.OnOutput != nil {
			m.OnOutput(name, line)
		}
	})
	defer lb.Flush()
	_, _ = io.Copy(lb, r)
}

// Stop gracefully stops a process by killing its entire process group.
func (m *Manager) Stop(name string, gracePeriod time.Duration) error {
	m.mu.RLock()
	owner := m.executions[name]
	m.mu.RUnlock()
	if owner != nil {
		return m.stopExecution(name, owner, gracePeriod)
	}

	m.mu.RLock()
	mp, exists := m.processes[name]
	m.mu.RUnlock()

	if !exists {
		return nil // already stopped
	}

	m.narrate(name, fmt.Sprintf("stopping process (pgid=%d, grace=%s)", mp.PGID, gracePeriod))
	if err := mp.requestGroupTermination(gracePeriod); err != nil {
		m.narrate(name, "ERROR: "+err.Error())
		return err
	}
	if mp.Cmd == nil {
		// Reconnected processes are poll-monitored because this daemon is not
		// their parent. KillGroup already confirmed the group is gone, so
		// waiting for the next one-second poll would add pure recovery latency.
		m.mu.Lock()
		if m.processes[name] == mp {
			delete(m.processes, name)
		}
		m.mu.Unlock()
		mp.CloseDone()
		return nil
	}

	// Wait for the process to finish
	select {
	case <-mp.Done:
		mp.mu.Lock()
		exitErr := mp.Err
		mp.mu.Unlock()
		if errors.Is(exitErr, ErrOutputDrainTimeout) {
			return exitErr
		}
	case <-time.After(gracePeriod + 2*time.Second):
		return fmt.Errorf("process %q did not exit after kill", name)
	}

	return nil
}

// StopAll stops all managed processes.
func (m *Manager) StopAll(gracePeriod time.Duration) {
	m.mu.RLock()
	names := make([]string, 0, len(m.processes)+len(m.executions))
	for name := range m.executions {
		names = append(names, name)
	}
	for name := range m.processes {
		if m.executions[name] == nil {
			names = append(names, name)
		}
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			_ = m.Stop(n, gracePeriod)
		}(name)
	}
	wg.Wait()
}

// IsRunning checks if a process is currently running.
func (m *Manager) IsRunning(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.processes[name]
	return exists || m.executions[name] != nil
}

// IsAlive checks if the managed process for the given name is still running.
func (m *Manager) IsAlive(name string) bool {
	m.mu.RLock()
	mp, exists := m.processes[name]
	m.mu.RUnlock()

	if !exists {
		return false
	}

	// For reconnected processes, check the reconnected PID
	pid := mp.ReconnectedPID
	if mp.Cmd != nil {
		pid = mp.Cmd.Process.Pid
	}
	if pid <= 0 {
		return false
	}
	return platform.IsProcessAlive(pid)
}

// List returns names of all running processes.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.processes)+len(m.executions))
	for name := range m.executions {
		names = append(names, name)
	}
	for name := range m.processes {
		if m.executions[name] == nil {
			names = append(names, name)
		}
	}
	return names
}

// Reconnect attaches to an existing process by PID and PGID.
// Used after daemon restart to resume monitoring a still-alive process.
// Returns an error if the process is not alive.
func (m *Manager) Reconnect(name string, pid, pgid int) error {
	// Verify the process is alive
	if !platform.IsProcessAlive(pid) {
		return fmt.Errorf("process %q (pid=%d) is not alive", name, pid)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.processes[name]; exists {
		return fmt.Errorf("process %q already tracked", name)
	}
	if m.executions[name] != nil {
		return fmt.Errorf("process %q already tracked", name)
	}

	mp := &ManagedProcess{
		Name:           name,
		PGID:           pgid,
		ReconnectedPID: pid,
		Started:        time.Now(), // approximate — we don't know the original start time
		Done:           make(chan struct{}),
	}

	m.processes[name] = mp

	// Monitor the process in background (poll-based since we don't own the process)
	go m.monitorReconnected(name, pid, mp)

	slog.Info("reconnected", "component", "process", "name", name, "pid", pid, "pgid", pgid)
	return nil
}

// monitorReconnected polls a reconnected process to detect exit.
func (m *Manager) monitorReconnected(name string, pid int, mp *ManagedProcess) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-mp.Done:
			return
		case <-ticker.C:
			if !platform.IsProcessAlive(pid) {
				// Process is dead
				m.mu.Lock()
				if m.processes[name] == mp {
					delete(m.processes, name)
				}
				m.mu.Unlock()
				mp.CloseDone()

				slog.Info("reconnected process exited", "component", "process", "name", name, "pid", pid)

				if m.OnExit != nil {
					m.OnExit(name, mp.Epoch, fmt.Errorf("process exited (detected via poll)"), nil)
				}
				return
			}
		}
	}
}

// GetProcessInfo returns the PID and PGID for a managed process.
// Used for state persistence.
func (m *Manager) GetProcessInfo(name string) (pid, pgid int, ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mp, exists := m.processes[name]
	if !exists {
		return 0, 0, false
	}

	// For reconnected processes, Cmd is nil
	if mp.Cmd != nil && mp.Cmd.Process != nil {
		return mp.Cmd.Process.Pid, mp.PGID, true
	}
	// Reconnected process
	return mp.ReconnectedPID, mp.PGID, true
}

// GetProcess returns the ManagedProcess for the given name, if any.
func (m *Manager) GetProcess(name string) (*ManagedProcess, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mp, ok := m.processes[name]
	return mp, ok
}
