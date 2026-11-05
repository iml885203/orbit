//go:build windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	jobObjects = make(map[int]windows.Handle) // pid → job object handle
	jobMu      sync.Mutex
)

// SetProcessGroup configures the command to create a new process group
// and assigns it to a Job Object after start. Call AssignJobObject after cmd.Start().
func SetProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// AssignJobObject creates a Job Object and assigns the process to it.
// Must be called after cmd.Start(). The Job Object ensures all child
// processes are killed when the job is terminated or the handle is closed.
func AssignJobObject(pid int) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("CreateJobObject: %w", err)
	}

	// Set KILL_ON_JOB_CLOSE so children die if the parent crashes
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("SetInformationJobObject: %w", err)
	}

	// Open the process handle
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("OpenProcess(%d): %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	if err := windows.AssignProcessToJobObject(job, handle); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}

	jobMu.Lock()
	jobObjects[pid] = job
	jobMu.Unlock()

	return nil
}

// GetProcessGroup returns the Job Object handle for the given PID.
// On Windows, the "pgid" is the PID used as key for the job object registry.
func GetProcessGroup(pid int) (int, error) {
	jobMu.Lock()
	defer jobMu.Unlock()
	if _, ok := jobObjects[pid]; ok {
		return pid, nil
	}
	// No job object — return pid as fallback (used as key)
	return pid, nil
}

// KillProcessGroup terminates all processes in the Job Object.
// The sig parameter is ignored on Windows.
func KillProcessGroup(pgid int, _ syscall.Signal) error {
	jobMu.Lock()
	job, ok := jobObjects[pgid]
	jobMu.Unlock()

	if !ok {
		// No job object — try to kill the process directly
		return terminateProcess(uint32(pgid))
	}

	if err := windows.TerminateJobObject(job, 1); err != nil {
		return fmt.Errorf("TerminateJobObject: %w", err)
	}

	jobMu.Lock()
	delete(jobObjects, pgid)
	jobMu.Unlock()

	_ = windows.CloseHandle(job)
	return nil
}

// IsGroupAlive checks if the process group (Job Object) still has live processes.
func IsGroupAlive(pgid int) bool {
	return IsProcessAlive(pgid)
}

// IsProcessAlive checks if a process with the given PID exists and is running.
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == 259 // STILL_ACTIVE
}

// DetachProcess configures the command to run detached from the parent,
// in a new process group.
func DetachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// SendTermSignal attempts to gracefully stop a process.
// Windows does not have SIGTERM, so we use taskkill without /F.
func SendTermSignal(pid int) error {
	return exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid)).Run()
}

// SendKillSignal forcefully terminates a process (/F on Windows).
func SendKillSignal(pid int) error {
	return exec.Command("taskkill", "/F", "/PID", fmt.Sprintf("%d", pid)).Run()
}

// terminateProcess forcefully kills a single process by PID.
func terminateProcess(pid uint32) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

// CleanupJobObject removes and closes the Job Object for a PID.
// Called when a process exits normally.
func CleanupJobObject(pid int) {
	jobMu.Lock()
	job, ok := jobObjects[pid]
	if ok {
		delete(jobObjects, pid)
	}
	jobMu.Unlock()

	if ok {
		_ = windows.CloseHandle(job)
	}
}

// ExecReplace runs the given command as a subprocess and exits with its code.
// Windows does not support Unix exec overlay, so we run as a child process.
func ExecReplace(name string, args []string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	os.Exit(0)
	return nil // unreachable
}
