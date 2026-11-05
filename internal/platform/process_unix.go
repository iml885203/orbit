//go:build !windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// SetProcessGroup configures the command to run in its own process group.
func SetProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// GetProcessGroup returns the process group ID for the given PID.
func GetProcessGroup(pid int) (int, error) {
	return syscall.Getpgid(pid)
}

// KillProcessGroup sends a signal to the entire process group.
// On Unix, this sends to -pgid (negative = group).
func KillProcessGroup(pgid int, sig syscall.Signal) error {
	return syscall.Kill(-pgid, sig)
}

// IsGroupAlive checks if the process group still has live processes.
func IsGroupAlive(pgid int) bool {
	return syscall.Kill(-pgid, 0) != syscall.ESRCH
}

// IsProcessAlive checks if a process with the given PID exists.
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// DetachProcess configures the command to run in a new session,
// detached from the parent terminal.
func DetachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// SendTermSignal sends a SIGTERM to the given process.
func SendTermSignal(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

// SendKillSignal sends a SIGKILL to the given process.
func SendKillSignal(pid int) error {
	return syscall.Kill(pid, syscall.SIGKILL)
}

// AssignJobObject is a no-op on Unix. Process groups are set via Setpgid.
func AssignJobObject(pid int) error {
	return nil
}

// CleanupJobObject is a no-op on Unix.
func CleanupJobObject(pid int) {}

// ExecReplace replaces the current process with the given command (Unix exec).
func ExecReplace(name string, args []string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found: %w", name, err)
	}
	argv := append([]string{name}, args...)
	return syscall.Exec(path, argv, os.Environ())
}
