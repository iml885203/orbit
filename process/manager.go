package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/iml885203/orbit/logging"
	"github.com/iml885203/orbit/platform"
)

// Manager manages child processes with process group isolation.
type Manager struct {
	processes map[string]*ManagedProcess
	mu        sync.RWMutex

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
	OnExit func(name string, epoch int, err error, stderr []string)
	// OnStarted runs after ownership is registered. The daemon uses it to
	// persist PID/PGID before health can be reported, closing the crash
	// window where a live child could otherwise become anonymous.
	OnStarted func(name string)
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
		processes: make(map[string]*ManagedProcess),
	}
}

// Start launches a process in its own process group. The process tree is
// bound to ctx: cancellation kills the whole group (not just the leader),
// which is what prevents zombies when a stop is issued mid-startup.
// Start launches the service process. epoch is an opaque caller token echoed
// back on OnExit — the engine passes the service generation so late exit
// notifications can't be attributed to a newer process.
func (m *Manager) Start(ctx context.Context, name, dir, command string, env map[string]string, preStart []string, epoch int) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start %q aborted: %w", name, err)
	}
	m.mu.Lock()
	if _, exists := m.processes[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("process %q already running", name)
	}
	m.mu.Unlock()

	// Run pre-start commands. Output streams into the service log via the
	// same OnOutput path the main process uses, so `orbit logs <service>`
	// reveals hangs and failures inside pre_start.
	for _, pre := range preStart {
		if err := m.runPreStart(ctx, name, dir, pre, env); err != nil {
			return err
		}
	}

	parts, err := commandArgs(command, env)
	if err != nil {
		return fmt.Errorf("command for %q: %w", name, err)
	}
	if len(parts) == 0 {
		return fmt.Errorf("empty command for %q", name)
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = dir

	cmd.Env = commandEnvironment(env)

	// KEY: Set process group so we can kill the entire tree
	platform.SetProcessGroup(cmd)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe for %q: %w", name, err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe for %q: %w", name, err)
	}

	m.narrate(name, fmt.Sprintf("starting process (cmd=%q, dir=%q)", command, dir))
	if err := cmd.Start(); err != nil {
		m.narrate(name, "ERROR: "+err.Error())
		return fmt.Errorf("starting %q (cmd=%q dir=%q): %w", name, command, dir, err)
	}
	slog.Info("started", "component", "process", "name", name, "pid", cmd.Process.Pid, "cmd", command, "dir", dir)
	m.narrate(name, fmt.Sprintf("started (pid=%d)", cmd.Process.Pid))

	// Assign to Job Object (Windows) — ensures child process tree is killed on stop.
	// No-op on Unix where process groups handle this.
	if err := platform.AssignJobObject(cmd.Process.Pid); err != nil {
		slog.Warn("AssignJobObject failed", "component", "process", "name", name, "err", err)
	}

	pgid, err := platform.GetProcessGroup(cmd.Process.Pid)
	if err != nil {
		pgid = cmd.Process.Pid // fallback
	}

	mp := &ManagedProcess{
		Name:    name,
		Cmd:     cmd,
		PGID:    pgid,
		Started: time.Now(),
		Done:    make(chan struct{}),
		Epoch:   epoch,
	}

	m.mu.Lock()
	m.processes[name] = mp
	m.mu.Unlock()
	if m.OnStarted != nil {
		m.OnStarted(name)
	}

	grace := m.CancelGrace
	if grace <= 0 {
		grace = defaultCancelGrace
	}
	// exec.CommandContext only SIGKILLs the leader, which leaves forked
	// grandchildren orphaned. Killing the whole group on cancel is what
	// shuts down the tree.
	go func() {
		select {
		case <-ctx.Done():
			_ = mp.stopGroup(grace)
		case <-mp.Done:
		}
	}()

	// Surface a cancellation that arrived mid-fork as a synchronous error
	// so the caller doesn't see nil and assume success. The watcher above
	// still handles the actual kill.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start %q aborted: %w", name, err)
	}

	// Keep stderr scoped to this process generation. The combined log remains
	// the user-facing stream; the separate buffer exists only so failure
	// summaries do not guess causality from ordinary stdout traffic.
	stderr := logging.NewRingBuffer(20)
	var outputWG sync.WaitGroup
	outputWG.Add(2)
	go func() {
		defer outputWG.Done()
		m.streamOutput(name, stdoutPipe, nil)
	}()
	go func() {
		defer outputWG.Done()
		m.streamOutput(name, stderrPipe, stderr.Write)
	}()

	// Wait for exit in background
	go func() {
		mp.mu.Lock()
		mp.Err = cmd.Wait()
		mp.mu.Unlock()
		outputWG.Wait()

		if mp.Err != nil {
			slog.Warn("exited", "component", "process", "name", name, "err", mp.Err)
		} else {
			slog.Info("exited", "component", "process", "name", name)
		}
		if mp.Err != nil {
			m.narrate(name, "exited: "+mp.Err.Error())
		} else {
			m.narrate(name, "exited")
		}
		platform.CleanupJobObject(cmd.Process.Pid)

		m.mu.Lock()
		if m.processes[name] == mp {
			delete(m.processes, name)
		}
		m.mu.Unlock()
		// Stop waits on Done before allowing a replacement to start. Close it
		// only after removing this exact generation from the manager so a
		// restart cannot collide with stale bookkeeping.
		mp.CloseDone()

		if m.OnExit != nil {
			m.OnExit(name, mp.Epoch, mp.Err, stderr.Lines())
		}
	}()

	return nil
}

// runPreStart executes one pre_start command for the named service. stdout
// and stderr are streamed line-by-line into the service log (via OnOutput),
// bracketed by synthetic `[pre_start] $ <cmd>` and `[pre_start] exit N`
// lines so a user reading `orbit logs <service>` can see exactly what the
// pre_start did and where it stopped.
func (m *Manager) runPreStart(
	ctx context.Context,
	name string,
	dir string,
	pre string,
	env map[string]string,
) error {
	parts, err := commandArgs(pre, env)
	if err != nil {
		return fmt.Errorf("pre_start for %q: %w", name, err)
	}
	if len(parts) == 0 {
		return nil
	}

	m.narrate(name, fmt.Sprintf("running pre_start: %s", pre))
	m.emitLine(name, fmt.Sprintf("[pre_start] $ %s", pre))

	preCmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	preCmd.Dir = dir
	preCmd.Env = commandEnvironment(env)

	stdoutPipe, err := preCmd.StdoutPipe()
	if err != nil {
		m.emitLine(name, fmt.Sprintf("[pre_start] pipe error: %v", err))
		return fmt.Errorf("pre_start %q stdout pipe: %w", pre, err)
	}
	stderrPipe, err := preCmd.StderrPipe()
	if err != nil {
		_ = stdoutPipe.Close()
		m.emitLine(name, fmt.Sprintf("[pre_start] pipe error: %v", err))
		return fmt.Errorf("pre_start %q stderr pipe: %w", pre, err)
	}

	if err := preCmd.Start(); err != nil {
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		m.emitLine(name, fmt.Sprintf("[pre_start] start error: %v", err))
		return fmt.Errorf("pre_start %q start: %w", pre, err)
	}

	// Mirror the main-process path: one streamOutput goroutine per pipe.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); m.streamOutput(name, stdoutPipe, nil) }()
	go func() { defer wg.Done(); m.streamOutput(name, stderrPipe, nil) }()
	wg.Wait()

	waitErr := preCmd.Wait()
	if waitErr != nil {
		exitCode := -1
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			exitCode = ee.ExitCode()
		}
		if exitCode >= 0 {
			m.emitLine(name, fmt.Sprintf("[pre_start] exit %d", exitCode))
		} else {
			m.emitLine(name, fmt.Sprintf("[pre_start] exit %d: %v", exitCode, waitErr))
		}
		m.narrate(name, fmt.Sprintf("pre_start failed (exit=%d): %s", exitCode, pre))
		return fmt.Errorf("pre_start %q failed: %w", pre, waitErr)
	}

	m.emitLine(name, "[pre_start] exit 0")
	m.narrate(name, fmt.Sprintf("pre_start ok: %s", pre))
	return nil
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
	mp, exists := m.processes[name]
	m.mu.RUnlock()

	if !exists {
		return nil // already stopped
	}

	m.narrate(name, fmt.Sprintf("stopping process (pgid=%d, grace=%s)", mp.PGID, gracePeriod))
	if err := mp.stopGroup(gracePeriod); err != nil {
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
	case <-time.After(gracePeriod + 2*time.Second):
		return fmt.Errorf("process %q did not exit after kill", name)
	}

	return nil
}

// StopAll stops all managed processes.
func (m *Manager) StopAll(gracePeriod time.Duration) {
	m.mu.RLock()
	names := make([]string, 0, len(m.processes))
	for name := range m.processes {
		names = append(names, name)
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
	return exists
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
	names := make([]string, 0, len(m.processes))
	for name := range m.processes {
		names = append(names, name)
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
