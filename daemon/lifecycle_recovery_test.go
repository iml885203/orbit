package daemon

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/iml885203/orbit/platform"
	"github.com/iml885203/orbit/process"
)

func TestDashboardListenerProvesDaemonOwnership(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	listener, port := occupyPort(t)
	defer func() { _ = listener.Close() }()
	t.Setenv("ORBIT_DASHBOARD_PORT", strconv.Itoa(port))

	if len(process.FindPortHolders([]int{port})) == 0 {
		t.Skip("port-holder lookup unavailable on this platform")
	}
	if err := os.MkdirAll(OrbitDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WritePID(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORBIT_DASHBOARD_PORT", strconv.Itoa(port+1))
	record := readPIDRecord()
	if record.PID != os.Getpid() || record.DashboardPort != port {
		t.Fatalf("record = %+v, want pid %d on port %d", record, os.Getpid(), port)
	}
	if !daemonOwnsDashboardPort(record.PID, record.DashboardPort) {
		t.Fatal("current process owns the dashboard listener but ownership was not proven")
	}
	if daemonOwnsDashboardPort(os.Getpid()+100000, record.DashboardPort) {
		t.Fatal("unrelated live PID was accepted as the dashboard owner")
	}
}

func TestReadPIDAcceptsLegacyPlainNumber(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	t.Setenv("ORBIT_DASHBOARD_PORT", "23456")
	if err := os.MkdirAll(OrbitDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DefaultPIDPath(), []byte("1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := readPIDRecord()
	if record.PID != 1234 || record.DashboardPort != 23456 {
		t.Fatalf("legacy record = %+v", record)
	}
}

func TestRetireUnreachableDaemonWaitsForProcessExit(t *testing.T) {
	if os.Getenv("ORBIT_DAEMON_RETIREMENT_HELPER") == "1" {
		for {
			time.Sleep(time.Second)
		}
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestRetireUnreachableDaemonWaitsForProcessExit")
	cmd.Env = append(os.Environ(), "ORBIT_DAEMON_RETIREMENT_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	t.Cleanup(func() {
		if platform.IsProcessAlive(pid) {
			_ = platform.SendKillSignal(pid)
			_ = cmd.Process.Kill()
		}
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			t.Errorf("helper pid %d did not exit during cleanup", pid)
		}
	})

	if err := retireUnreachableDaemon(pid); err != nil {
		t.Fatal(err)
	}
	if platform.IsProcessAlive(pid) {
		t.Fatalf("retirement returned while pid %d was still alive", pid)
	}
}
