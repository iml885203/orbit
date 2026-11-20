//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	jobObjects = make(map[int]*ownedJob)
	jobMu      sync.Mutex
)

const jobRetirementTimeout = 2 * time.Second

var ErrJobRetirementTimeout = errors.New("windows job retirement could not be confirmed before deadline")

type ownedJob struct {
	mu      sync.Mutex
	handle  windows.Handle
	retired bool
	budget  time.Duration
	members map[uint32]windows.Handle
	census  func(windows.Handle, time.Time) ([]uint32, error)
}

func registeredJob(pid int) *ownedJob {
	jobMu.Lock()
	defer jobMu.Unlock()
	return jobObjects[pid]
}

// SetProcessGroup configures the command to create a new process group
// suspended: assign the job and publish ownership before ResumeProcess.
func SetProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED,
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
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("OpenProcess(%d): %w", pid, err)
	}

	jobMu.Lock()
	if jobObjects[pid] != nil {
		jobMu.Unlock()
		_ = windows.CloseHandle(handle)
		_ = windows.CloseHandle(job)
		return fmt.Errorf("job ownership for pid %d is already registered", pid)
	}
	if err := windows.AssignProcessToJobObject(job, handle); err != nil {
		jobMu.Unlock()
		_ = windows.CloseHandle(handle)
		_ = windows.CloseHandle(job)
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	jobObjects[pid] = &ownedJob{handle: job, budget: jobRetirementTimeout, members: map[uint32]windows.Handle{uint32(pid): handle}}
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
	job := registeredJob(pgid)
	if job == nil {
		return terminateProcess(uint32(pgid))
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.retired {
		return nil
	}
	if job.handle == 0 {
		return fmt.Errorf("job ownership for %d has no open handle", pgid)
	}
	return job.retire(time.Now().Add(job.budget))
}

func IsGroupAlive(pgid int) bool {
	job := registeredJob(pgid)
	if job == nil {
		return IsProcessAlive(pgid)
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	return !job.retired
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
	job := registeredJob(pid)
	if job == nil {
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if !job.retired {
		return
	}
	jobMu.Lock()
	if jobObjects[pid] == job {
		delete(jobObjects, pid)
	}
	jobMu.Unlock()
	for pid, handle := range job.members {
		_ = windows.CloseHandle(handle)
		delete(job.members, pid)
	}
	if job.handle != 0 {
		_ = windows.CloseHandle(job.handle)
		job.handle = 0
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

// Go closes CreateProcess's initial thread handle before Cmd.Start returns.
// The initial thread is still suspended, so enumeration cannot race user code
// creating additional threads before job membership and ownership publication.
func ResumeProcess(pid int) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("snapshot suspended threads: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(pid) {
			continue
		}
		thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if openErr != nil {
			return fmt.Errorf("open initial thread: %w", openErr)
		}
		_, resumeErr := windows.ResumeThread(thread)
		_ = windows.CloseHandle(thread)
		if resumeErr != nil {
			return fmt.Errorf("resume owned process: %w", resumeErr)
		}
		return nil
	}
	return fmt.Errorf("initial thread for suspended process %d: %w", pid, err)
}
