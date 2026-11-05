//go:build !windows

package platform

import (
	"os"
	"os/exec"
	"testing"
)

func TestIsProcessAlive_CurrentProcess(t *testing.T) {
	pid := os.Getpid()
	if !IsProcessAlive(pid) {
		t.Errorf("expected current process (pid %d) to be alive", pid)
	}
}

func TestIsProcessAlive_DeadProcess(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}
	pid := cmd.Process.Pid

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("failed to kill process: %v", err)
	}
	// Wait reaps the zombie so the OS reclaims the PID.
	_ = cmd.Wait()

	if IsProcessAlive(pid) {
		t.Errorf("expected killed process (pid %d) to be dead", pid)
	}
}

func TestGetProcessGroup_CurrentProcess(t *testing.T) {
	pgid, err := GetProcessGroup(os.Getpid())
	if err != nil {
		t.Fatalf("GetProcessGroup failed: %v", err)
	}
	if pgid == 0 {
		t.Error("expected non-zero process group ID")
	}
}
