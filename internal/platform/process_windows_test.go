//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestOwnedJobProcessHelper(t *testing.T) {
	if os.Getenv("ORBIT_JOB_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "child" || mode == "leaf" {
		if err := os.WriteFile(os.Getenv("ORBIT_JOB_READY"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if mode == "child" {
				gate := os.Getenv("ORBIT_JOB_READY") + ".spawn"
				if intent, err := os.ReadFile(gate); err == nil {
					late := exec.Command(os.Args[0], "-test.run=^TestOwnedJobProcessHelper$", "-test.timeout=20s", "--", "leaf")
					late.Env = os.Environ()
					if err := late.Start(); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(gate+".pid", []byte(strconv.Itoa(late.Process.Pid)), 0600); err != nil {
						t.Fatal(err)
					}
					if string(intent) == "exit" {
						return
					}
					mode = "leaf"
				}
			}
			time.Sleep(time.Millisecond)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedJobProcessHelper$", "-test.timeout=20s", "--", "child")
	cmd.Env = os.Environ()
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	cmd.Stdout, cmd.Stderr = null, null
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("ORBIT_JOB_PID"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("ORBIT_JOB_READY")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("descendant not ready")
		}
		time.Sleep(time.Millisecond)
	}
	if mode == "hold_leader" {
		time.Sleep(15 * time.Second)
	}
}

func startOwnedJobFixture(t *testing.T, keepLeader ...bool) (int, int, string) {
	t.Helper()
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "pid")
	readyPath := filepath.Join(dir, "ready")
	mode := "leader"
	if len(keepLeader) > 0 && keepLeader[0] {
		mode = "hold_leader"
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedJobProcessHelper$", "-test.timeout=20s", "--", mode)
	cmd.Env = append(os.Environ(), "ORBIT_JOB_HELPER=1", "ORBIT_JOB_PID="+pidPath, "ORBIT_JOB_READY="+readyPath)
	SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	root := cmd.Process.Pid
	if err := AssignJobObject(root); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	owner := registeredJob(root)
	t.Cleanup(func() {
		if registeredJob(root) != owner {
			return
		}
		owner.mu.Lock()
		if registeredJob(root) != owner {
			owner.mu.Unlock()
			return
		}
		owner.census = nil
		owner.budget = jobRetirementTimeout
		var err error
		if !owner.retired {
			err = owner.retire(time.Now().Add(owner.budget))
		}
		owner.mu.Unlock()
		if err != nil {
			t.Errorf("retire fixture's exact job: %v", err)
			return
		}
		CleanupJobObject(root)
	})
	if err := ResumeProcess(root); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	if mode == "hold_leader" {
		go func() { _ = cmd.Wait() }()
		deadline := time.Now().Add(5 * time.Second)
		for {
			_, readyErr := os.Stat(readyPath)
			_, pidErr := os.Stat(pidPath)
			if readyErr == nil && pidErr == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("descendant readiness timeout")
			}
			time.Sleep(time.Millisecond)
		}
	} else if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	descendant, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}

	return root, descendant, readyPath
}

func TestOwnedJobLivenessIncludesDescendantAfterLeaderExit(t *testing.T) {
	root, child, _ := startOwnedJobFixture(t)
	if IsProcessAlive(root) {
		t.Fatal("leader should already be reaped")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(child))
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = windows.CloseHandle(handle)
		}
	}()
	originalPID, err := windows.GetProcessId(handle)
	if err != nil || originalPID != uint32(child) {
		t.Fatalf("original descendant pid=%d err=%v", originalPID, err)
	}
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		t.Fatal(err)
	}
	job := registeredJob(root)
	if job == nil {
		t.Fatal("owned job missing")
	}
	var member int32
	job.mu.Lock()
	originalJob := job.handle
	memberOK, _, memberErr := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob").Call(uintptr(handle), uintptr(originalJob), uintptr(unsafe.Pointer(&member)))
	beforeActive, beforeErr := job.active()
	job.mu.Unlock()
	if memberOK == 0 || member == 0 {
		t.Fatalf("original child is not a member of exact owned job: member=%d err=%v", member, memberErr)
	}
	beforeSignal, err := windows.WaitForSingleObject(handle, 0)
	if err != nil || beforeSignal != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("original descendant was not alive before termination: wait=%d err=%v", beforeSignal, err)
	}
	if !IsGroupAlive(root) {
		t.Fatal("job liveness ignored the remaining descendant")
	}
	result := KillProcessGroup(root, syscall.SIGTERM)
	afterSignal, waitErr := windows.WaitForSingleObject(handle, 0)
	job.mu.Lock()
	afterActive, afterErr := job.active()
	retained := job.handle == originalJob
	job.mu.Unlock()
	t.Logf("pinned descendant pid=%d created=%d:%d member=%d before_wait=%d before_active=%d before_query_error=%v after_wait=%d after_active=%d after_query_error=%v termination_error=%v", originalPID, created.HighDateTime, created.LowDateTime, member, beforeSignal, beforeActive, beforeErr, afterSignal, afterActive, afterErr, result)
	if waitErr != nil {
		t.Fatal(waitErr)
	}
	if result == nil {
		if afterSignal != windows.WAIT_OBJECT_0 {
			t.Fatalf("successful retirement returned before ORIGINAL pinned descendant exit: wait=%d", afterSignal)
		}
	} else {
		if !errors.Is(result, ErrJobRetirementTimeout) {
			t.Fatalf("unexpected retirement failure: %v", result)
		}
		if registeredJob(root) != job || !retained {
			t.Fatal("bounded uncertainty lost exact job identity")
		}
	}
	// The process reference is deliberately retained across the strict oracle.
	// A conservative failed attempt retains ownership for an explicit retry.
	_ = windows.CloseHandle(handle)
	closed = true
	if result != nil {
		if err := KillProcessGroup(root, syscall.SIGTERM); err != nil {
			t.Fatal("retry after releasing original process handle:", err)
		}
	}
	if IsGroupAlive(root) {
		t.Fatal("completed retirement still reports active job")
	}
}

func TestOwnedJobRetirementWithoutExternalReferencesSucceeds(t *testing.T) {
	root, _, _ := startOwnedJobFixture(t)
	if err := KillProcessGroup(root, syscall.SIGTERM); err != nil {
		t.Fatal("ordinary job retirement:", err)
	}
	if IsGroupAlive(root) {
		t.Fatal("ordinary successful retirement left active job")
	}
}

func TestJobRetirementUncertaintyRetainsIdentityForRetry(t *testing.T) {
	for _, failureMode := range []string{"query", "deadline"} {
		t.Run(failureMode, func(t *testing.T) {
			root, _, _ := startOwnedJobFixture(t)
			job := registeredJob(root)
			originalHandle := job.handle
			fault := errors.New("controlled job-member census failure")
			job.mu.Lock()
			if failureMode == "query" {
				job.census = func(windows.Handle, time.Time) ([]uint32, error) { return nil, fault }
			} else {
				job.budget = 0
			}
			job.mu.Unlock()
			err := KillProcessGroup(root, syscall.SIGTERM)
			if failureMode == "query" && !errors.Is(err, fault) {
				t.Fatalf("query error=%v", err)
			}
			if failureMode == "deadline" && !errors.Is(err, ErrJobRetirementTimeout) {
				t.Fatalf("deadline error=%v", err)
			}
			if registeredJob(root) != job || job.handle != originalHandle {
				t.Fatal("uncertain retirement discarded exact job")
			}
			if !IsGroupAlive(root) {
				t.Fatal("uncertainty became false dead claim")
			}
			CleanupJobObject(root)
			if registeredJob(root) != job {
				t.Fatal("cleanup erased unproven job")
			}
			job.mu.Lock()
			job.census = nil
			job.budget = jobRetirementTimeout
			job.mu.Unlock()
			if err := KillProcessGroup(root, syscall.SIGTERM); err != nil {
				t.Fatal("retry:", err)
			}
			CleanupJobObject(root)
			if registeredJob(root) != nil {
				t.Fatal("proven retired job not removed")
			}
		})
	}
}

func TestDuplicateJobRegistrationPreservesExistingOwner(t *testing.T) {
	root, _, _ := startOwnedJobFixture(t, true)
	original := registeredJob(root)
	// Duplicate registration must not overwrite a live process generation.
	err := AssignJobObject(root)
	if err == nil {
		t.Fatal("duplicate job accepted")
	}
	if registeredJob(root) != original || !IsGroupAlive(root) {
		t.Fatalf("duplicate registration lost owner: %v", err)
	}
}

func TestJobRetirementDiscoversDescendantBornAfterCensus(t *testing.T) {
	for _, naturalExit := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent_natural_exit_%t", naturalExit), func(t *testing.T) {
			exerciseLateJobBirth(t, naturalExit)
		})
	}
}

func exerciseLateJobBirth(t *testing.T, naturalExit bool) {
	t.Helper()
	root, parent, ready := startOwnedJobFixture(t)
	job := registeredJob(root)
	parentHandle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(parent))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(parentHandle)
	var lateHandle windows.Handle
	calls := 0
	job.census = func(handle windows.Handle, deadline time.Time) ([]uint32, error) {
		ids, err := currentJobMembers(handle, deadline)
		if err != nil {
			return nil, err
		}
		calls++
		if calls != 1 {
			return ids, nil
		}
		gate := ready + ".spawn"
		intent := []byte("spawn")
		if naturalExit {
			intent = []byte("exit")
		}
		if err := os.WriteFile(gate, intent, 0600); err != nil {
			return nil, err
		}
		fixtureDeadline := time.Now().Add(time.Second)
		for time.Now().Before(fixtureDeadline) {
			data, err := os.ReadFile(gate + ".pid")
			if err == nil {
				pid, err := strconv.Atoi(string(data))
				if err != nil {
					return nil, err
				}
				lateHandle, err = windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
				if err != nil {
					return nil, err
				}
				if naturalExit {
					status, err := windows.WaitForSingleObject(parentHandle, 1000)
					if err != nil {
						return nil, err
					}
					if status != windows.WAIT_OBJECT_0 {
						return nil, errors.New("fixture parent did not naturally exit")
					}
				}
				return ids, nil
			}
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			time.Sleep(time.Millisecond)
		}
		return nil, errors.New("late descendant fixture did not spawn")
	}
	err = KillProcessGroup(root, syscall.SIGTERM)
	if lateHandle != 0 {
		defer windows.CloseHandle(lateHandle)
	}
	if err != nil {
		t.Fatal(err)
	}
	if lateHandle == 0 || calls < 2 {
		t.Fatal("late birth fixture did not exercise repeated census")
	}
	status, err := windows.WaitForSingleObject(lateHandle, 0)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("late descendant not signaled at return: status=%d error=%v", status, err)
	}
}

func TestJobMemberAttestationRejectsForeignProcess(t *testing.T) {
	root, _, _ := startOwnedJobFixture(t)
	job := registeredJob(root)
	job.census = func(handle windows.Handle, deadline time.Time) ([]uint32, error) {
		ids, err := currentJobMembers(handle, deadline)
		if err != nil {
			return nil, err
		}
		return append(ids, uint32(os.Getpid())), nil
	}
	if err := KillProcessGroup(root, syscall.SIGTERM); err == nil {
		t.Fatal("foreign process admitted to owned job")
	}
	if !IsGroupAlive(root) || registeredJob(root) != job {
		t.Fatal("failed attestation discarded ownership")
	}
	if len(job.members) < 2 {
		t.Fatal("partial capture discarded previously pinned descendant")
	}
	originalPins := make(map[uint32]windows.Handle, len(job.members))
	for pid, handle := range job.members {
		originalPins[pid] = handle
	}
	job.census = nil
	if err := KillProcessGroup(root, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	for pid, handle := range originalPins {
		if job.members[pid] != handle {
			t.Fatal("retry replaced exact pinned process identity")
		}
	}

}

func TestJobRetirementDeadlineAfterCensusDoesNotTerminateMembers(t *testing.T) {
	root, child, _ := startOwnedJobFixture(t)
	job := registeredJob(root)
	job.budget = 20 * time.Millisecond
	job.census = func(handle windows.Handle, deadline time.Time) ([]uint32, error) {
		ids, err := currentJobMembers(handle, deadline)
		if err != nil {
			return nil, err
		}
		<-time.After(time.Until(deadline) + time.Millisecond)
		return ids, nil
	}
	if err := KillProcessGroup(root, syscall.SIGTERM); !errors.Is(err, ErrJobRetirementTimeout) {
		t.Fatalf("expired census: %v", err)
	}
	if len(job.members) != 1 || !IsProcessAlive(child) {
		t.Fatal("expired deadline still admitted or terminated a member")
	}
	job.census = nil
	job.budget = jobRetirementTimeout
	if err := KillProcessGroup(root, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
}

type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func queryActiveJobProcesses(handle windows.Handle) (uint32, error) {
	var accounting jobAccounting
	if err := windows.QueryInformationJobObject(handle, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil); err != nil {
		return 0, fmt.Errorf("query job accounting: %w", err)
	}
	return accounting.ActiveProcesses, nil
}

func (job *ownedJob) active() (uint32, error) {
	return queryActiveJobProcesses(job.handle)
}

func TestJobMemberCensusResizesAtThirtyThreeMembers(t *testing.T) {
	root, _, _ := startOwnedJobFixture(t, true)
	job := registeredJob(root)
	// The held leader and its child supply two members; suspended additions
	// force the native list to exceed the first buffer by exactly one entry.
	for range 31 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedJobProcessHelper$")
		SetProcessGroup(cmd)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
		if err != nil {
			t.Fatal(err)
		}
		err = windows.AssignProcessToJobObject(job.handle, handle)
		_ = windows.CloseHandle(handle)
		if err != nil {
			t.Fatal(err)
		}
	}
	ids, err := currentJobMembers(job.handle, time.Now().Add(jobRetirementTimeout))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 33 {
		t.Fatalf("native census returned %d members, want exactly 33", len(ids))
	}
	if err := KillProcessGroup(root, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
}
