package app

import (
	"os"
	"os/exec"
	"testing"

	"github.com/iml885203/orbit/internal/daemonsrv"
)

func TestInstanceSurvivorsCountsOnlyLiveRecordedProcesses(t *testing.T) {
	exited := exec.Command("true")
	if err := exited.Run(); err != nil {
		t.Skipf("no short-lived process available: %v", err)
	}
	prevState := &daemonsrv.DaemonState{Processes: map[string]daemonsrv.ProcessRecord{
		"api":    {PID: os.Getpid()},
		"worker": {PID: exited.Process.Pid},
	}}

	survivors := instanceSurvivors("", prevState, nil)

	if !survivors.Processes["api"] {
		t.Fatal("a live recorded process must count as a survivor")
	}
	if survivors.Processes["worker"] {
		t.Fatal("a recorded process that has exited must not hold its port")
	}
}
