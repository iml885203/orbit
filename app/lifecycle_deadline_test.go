package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/spf13/cobra"
)

func TestAgentLifecycleTimeoutHelpDistinguishesJSONAndHumanScope(t *testing.T) {
	const want = "maximum duration for the complete JSON operation; in human output, how long to wait for resources to settle (default 5m)"
	for name, command := range map[string]*cobra.Command{
		"up":        upCmd(),
		"env apply": envApplyCmd(),
	} {
		flag := command.Flags().Lookup("timeout")
		if flag == nil || flag.Usage != want {
			t.Errorf("%s timeout help = %#v, want %q", name, flag, want)
		}
	}
}

func TestUpJSONCatchableSignalWritesOneCanceledEnvelope(t *testing.T) {
	if os.Getenv("ORBIT_TEST_SIGNAL_ENVELOPE") == "1" {
		home := os.Getenv("ORBIT_TEST_HOME")
		_ = os.MkdirAll(home, 0o755)
		configFile = filepath.Join(home, "orbit.yaml")
		_ = os.WriteFile(configFile, []byte("version: \"3\"\n"), 0o600)
		_ = daemon.WritePID()
		listener, listenErr := net.Listen("unix", daemon.DefaultSocketPath())
		if listenErr != nil {
			_, _ = os.Stderr.WriteString(listenErr.Error() + "\n")
			os.Exit(2)
		}
		var blocked sync.Once
		server := &http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			blocked.Do(func() { _, _ = os.Stderr.WriteString("blocked\n") })
			<-r.Context().Done()
		})}
		go func() { _ = server.Serve(listener) }()
		cli.JSONOutput = true
		timeout = time.Minute
		cmd := upCmd()
		cmd.PersistentFlags().String("config", configFile, "")
		_ = cmd.PersistentFlags().Set("config", configFile)
		cmd.SetContext(context.Background())
		err := runUp(cmd, nil)
		printExecutionError(os.Stderr, err)
		os.Exit(1)
	}

	home, err := os.MkdirTemp("/tmp", "o115-signal-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	cmd := exec.Command(os.Args[0], "-test.run=TestUpJSONCatchableSignalWritesOneCanceledEnvelope")
	cmd.Env = append(os.Environ(), "ORBIT_TEST_SIGNAL_ENVELOPE=1", "ORBIT_TEST_HOME="+home, "ORBIT_HOME="+home)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stderr)
	var progressLines []string
	blocked := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "blocked" {
			blocked = true
			break
		}
		progressLines = append(progressLines, line)
	}
	if err := scanner.Err(); err != nil || !blocked || len(progressLines) == 0 || progressLines[0] != "⋯ ensuring daemon" {
		t.Fatalf("child progress before blocked RPC = %q, blocked = %v, scanner error = %v", progressLines, blocked, err)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("child exit = %v, want 1", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var envelope cli.JSONEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, stdout.Bytes())
	}
	if envelope.Error == nil || envelope.Error.Code != "canceled" || envelope.SchemaVersion != cli.SchemaVersion {
		t.Fatalf("envelope = %+v", envelope)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contained more than one envelope: %v\n%s", err, stdout.Bytes())
	}
}
