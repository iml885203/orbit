//go:build e2e

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/iml885203/orbit/platform"
)

func TestDaemonRecoveryUnrelatedProcessHelper(t *testing.T) {
	if os.Getenv("ORBIT_UNRELATED_PROCESS_HELPER") != "1" {
		return
	}
	ready := os.Getenv("ORBIT_UNRELATED_PROCESS_READY")
	if err := os.WriteFile(ready, []byte("ready"), 0o600); err != nil {
		os.Exit(2)
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestE2E_StaleDaemonMetadataNeverKillsUnrelatedProcess(t *testing.T) {
	t.Parallel()

	binary := findOrbitBinary(t)
	home, err := os.MkdirTemp("/tmp", "orb-stale-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })

	configPath := filepath.Join(t.TempDir(), "orbit.yaml")
	if err := os.WriteFile(configPath, []byte("version: \"3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dashboardPort := reserveLocalPort(t)
	readyPath := filepath.Join(home, "unrelated.ready")
	unrelated := exec.Command(os.Args[0], "-test.run=TestDaemonRecoveryUnrelatedProcessHelper")
	unrelated.Env = append(os.Environ(),
		"ORBIT_UNRELATED_PROCESS_HELPER=1",
		"ORBIT_UNRELATED_PROCESS_READY="+readyPath,
	)
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	unrelatedPID := unrelated.Process.Pid
	unrelatedDone := make(chan error, 1)
	go func() { unrelatedDone <- unrelated.Wait() }()
	t.Cleanup(func() {
		if platform.IsProcessAlive(unrelatedPID) {
			_ = platform.SendKillSignal(unrelatedPID)
			_ = unrelated.Process.Kill()
		}
		select {
		case <-unrelatedDone:
		case <-time.After(5 * time.Second):
			t.Errorf("unrelated helper pid %d did not exit during cleanup", unrelatedPID)
		}
	})
	readyDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(readyDeadline) {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(readyPath); err != nil {
		t.Fatalf("timed out waiting for unrelated process: %v", err)
	}

	record, err := json.Marshal(struct {
		PID           int `json:"pid"`
		DashboardPort int `json:"dashboard_port"`
	}{PID: unrelatedPID, DashboardPort: dashboardPort})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "orbit.pid"), append(record, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "orbit.sock"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	namespace := "e2e-stale-" + randHex(4)
	command := func(args ...string) *exec.Cmd {
		cmd := exec.Command(binary, append([]string{"-c", configPath}, args...)...)
		cmd.Env = append(os.Environ(),
			"ORBIT_HOME="+home,
			"ORBIT_NAMESPACE="+namespace,
			fmt.Sprintf("ORBIT_DASHBOARD_PORT=%d", dashboardPort),
		)
		return cmd
	}
	t.Cleanup(func() { _ = command("daemon", "stop", "--json").Run() })

	output, err := command("up", "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("up with stale daemon metadata: %v\n%s", err, output)
	}
	envelope := parseE2EEnvelope(t, string(output))
	if !envelope.OK {
		t.Fatalf("up envelope = %+v\n%s", envelope, output)
	}
	if !platform.IsProcessAlive(unrelatedPID) {
		t.Fatalf("Orbit killed unrelated pid %d from stale metadata", unrelatedPID)
	}

	currentRecord, err := os.ReadFile(filepath.Join(home, "orbit.pid"))
	if err != nil {
		t.Fatal(err)
	}
	var current struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(currentRecord, &current); err != nil {
		t.Fatalf("new daemon ownership is not structured JSON: %v\n%s", err, currentRecord)
	}
	if current.PID <= 0 || current.PID == unrelatedPID {
		t.Fatalf("new daemon pid = %d, unrelated pid = %d", current.PID, unrelatedPID)
	}
}
