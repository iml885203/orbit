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
	if !IsProcessAlive(child) {
		t.Fatal("descendant exited before liveness observation")
	}
	if !IsGroupAlive(root) {
		t.Fatal("job liveness ignored the remaining descendant")
	}
	if err := KillProcessGroup(root, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if IsGroupAlive(root) {
		t.Fatal("successful retirement left active job count")
	}
	// No external reference pins accounting during this normal success case.
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(child))
	if err == nil {
		defer windows.CloseHandle(handle)
		state, waitErr := windows.WaitForSingleObject(handle, 0)
		if waitErr != nil || state != windows.WAIT_OBJECT_0 {
			t.Fatalf("successful retirement returned before descendant exit: state=%d err=%v", state, waitErr)
		}
	} else if !errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		t.Fatal(err)
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
