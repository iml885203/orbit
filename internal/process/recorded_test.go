//go:build darwin || linux

package process

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/platform"
)

func startRecordedFixture(t *testing.T, dir string, args ...string) RecordedProcess {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	platform.SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() {
		_ = KillGroup(cmd.Process.Pid, 0)
		<-exited
	})
	pgid, err := platform.GetProcessGroup(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	rec := RecordedProcess{Name: "api", PID: cmd.Process.Pid, PGID: pgid, Dir: dir}
	waitForExec(t, rec.PID)
	return rec
}

// waitForExec waits until exec has replaced the forked child, so the argv the
// test asserts against is the program's rather than the test binary's.
func waitForExec(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if argv, err := platform.ProcessArgs(pid); err == nil && len(argv) > 0 && argv[0] != os.Args[0] {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid %d never exec'd its program", pid)
}

func TestRetireRecordedStopsTheProcessItRecorded(t *testing.T) {
	dir := t.TempDir()
	rec := startRecordedFixture(t, dir, "sleep", "60")
	rec.Command = "sleep 60"

	retired, err := RetireRecorded(rec, time.Second)
	if err != nil || !retired {
		t.Fatalf("RetireRecorded = %v, %v; want retired", retired, err)
	}
	if platform.IsGroupAlive(rec.PGID) {
		t.Fatal("recorded process group survived retirement")
	}
}

func TestRetireRecordedMatchesACommandThatReferencesServiceVariables(t *testing.T) {
	dir := t.TempDir()
	rec := startRecordedFixture(t, dir, "sleep", "60")
	rec.Command = "sleep ${DELAY}"

	if retired, err := RetireRecorded(rec, time.Second); err != nil || !retired {
		t.Fatalf("RetireRecorded = %v, %v; want retired", retired, err)
	}
}

func TestRetireRecordedMatchesAScriptRunByItsInterpreter(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "serve.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	rec := startRecordedFixture(t, dir, "./serve.sh")
	rec.Command = "./serve.sh"

	if retired, err := RetireRecorded(rec, time.Second); err != nil || !retired {
		t.Fatalf("RetireRecorded = %v, %v; want retired", retired, err)
	}
}

func TestRetireRecordedLeavesAnUnprovenProcessRunning(t *testing.T) {
	dir := t.TempDir()
	tests := map[string]func(*RecordedProcess){
		"different command":   func(r *RecordedProcess) { r.Command = "sleep 61" },
		"different directory": func(r *RecordedProcess) { r.Command = "sleep 60"; r.Dir = t.TempDir() },
		"different group":     func(r *RecordedProcess) { r.Command = "sleep 60"; r.PGID = os.Getpid() },
		"no recorded command": func(r *RecordedProcess) {},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			rec := startRecordedFixture(t, dir, "sleep", "60")
			mutate(&rec)

			retired, err := RetireRecorded(rec, time.Second)
			var mismatch *OwnershipMismatchError
			if retired || !errors.As(err, &mismatch) {
				t.Fatalf("RetireRecorded = %v, %v; want ownership mismatch", retired, err)
			}
			if !platform.IsProcessAlive(rec.PID) {
				t.Fatal("RetireRecorded killed a process it could not prove it owned")
			}
		})
	}
}

func TestRetireRecordedIgnoresAnExitedProcess(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	rec := RecordedProcess{Name: "api", PID: cmd.Process.Pid, PGID: cmd.Process.Pid, Command: "true", Dir: t.TempDir()}

	if retired, err := RetireRecorded(rec, time.Second); err != nil || retired {
		t.Fatalf("RetireRecorded = %v, %v; want nothing to retire", retired, err)
	}
}
