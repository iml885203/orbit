package app

import (
	"bytes"
	"testing"

	"github.com/iml885203/orbit/cli"
)

func TestBuildDaemonStartJSONData(t *testing.T) {
	got := buildDaemonJSONData(daemonJSONOptions{
		Operation:  "daemon_start",
		Running:    true,
		PID:        123,
		ConfigPath: "/tmp/env.yaml",
		Dashboard:  "http://localhost:19800",
	})

	if got.Operation != "daemon_start" {
		t.Fatalf("operation = %q", got.Operation)
	}
	if !got.Running {
		t.Fatal("running = false, want true")
	}
	if got.PID != 123 {
		t.Fatalf("pid = %d", got.PID)
	}
	if got.ConfigPath != "/tmp/env.yaml" {
		t.Fatalf("config_path = %q", got.ConfigPath)
	}
	if got.Dashboard != "http://localhost:19800" {
		t.Fatalf("dashboard = %q", got.Dashboard)
	}
}

func TestBuildDaemonStopJSONData(t *testing.T) {
	got := buildDaemonJSONData(daemonJSONOptions{
		Operation:                "daemon_stop",
		Running:                  false,
		PreviousPID:              456,
		RequestedServiceShutdown: true,
		StopMethod:               daemonStopGraceful,
	})

	if got.Operation != "daemon_stop" {
		t.Fatalf("operation = %q", got.Operation)
	}
	if got.Running {
		t.Fatal("running = true, want false")
	}
	if got.PreviousPID != 456 {
		t.Fatalf("previous_pid = %d", got.PreviousPID)
	}
	if !got.RequestedServiceShutdown {
		t.Fatal("requested_service_shutdown = false, want true")
	}
	if got.StopMethod != "graceful" {
		t.Fatalf("stop_method = %q", got.StopMethod)
	}
}

func TestPrintDaemonStopResultSuppressesJSONMode(t *testing.T) {
	origJSON := cli.JSONOutput
	t.Cleanup(func() { cli.JSONOutput = origJSON })
	cli.JSONOutput = true

	var buf bytes.Buffer
	printDaemonStopResult(&buf, daemonStopGraceful)

	if got := buf.String(); got != "" {
		t.Fatalf("JSON mode stop output = %q, want empty", got)
	}
}
