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
	if mode == "child" {
		if err := os.WriteFile(os.Getenv("ORBIT_JOB_READY"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Second)
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

func startOwnedJobFixture(t *testing.T, keepLeader ...bool) (int, int) {
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
	t.Cleanup(func() {
		if job := registeredJob(root); job != nil {
			job.mu.Lock()
			job.query = nil
			job.budget = jobRetirementTimeout
			job.mu.Unlock()
		}
		_ = KillProcessGroup(root, syscall.SIGKILL)
		CleanupJobObject(root)
		if p, err := os.FindProcess(descendant); err == nil {
			_ = p.Kill()
		}
	})
	return root, descendant
}

func TestOwnedJobLivenessIncludesDescendantAfterLeaderExit(t *testing.T) {
	root, child := startOwnedJobFixture(t)
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
	// Accounting may require releasing it before a conservative retry succeeds.
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
	root, _ := startOwnedJobFixture(t)
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
			root, _ := startOwnedJobFixture(t)
			job := registeredJob(root)
			originalHandle := job.handle
			fault := errors.New("controlled job-accounting query failure")
			job.mu.Lock()
			if failureMode == "query" {
				job.query = func(windows.Handle) (uint32, error) { return 0, fault }
			} else {
				job.query = func(windows.Handle) (uint32, error) { return 1, nil }
				job.budget = 20 * time.Millisecond
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
			job.query = nil
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
	root, _ := startOwnedJobFixture(t, true)
	original := registeredJob(root)
	// Duplicate registration must not overwrite a live process generation.
	err := AssignJobObject(root)
	if err == nil {
		t.Fatal("duplicate job accepted")
	}
	if registeredJob(root) != original || !IsGroupAlive(root) {
		t.Fatal(fmt.Sprintf("duplicate registration lost owner: %v", err))
	}
}
