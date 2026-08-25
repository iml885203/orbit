package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
)

func TestEnvApplyUsesLifecycleTimeoutFlag(t *testing.T) {
	flag := envApplyCmd().Flags().Lookup("timeout")
	if flag == nil || flag.DefValue != "0s" {
		t.Fatalf("timeout flag = %#v", flag)
	}
}

func TestLifecycleCancellationReportsAcceptedReconcileHandoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := lifecycleOperationError(ctx, errors.New("request failed"), true)
	if !errors.Is(err, cli.ErrCanceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
	if !strings.Contains(err.Error(), "daemon may have accepted") || !strings.Contains(err.Error(), "orbit status --json") {
		t.Fatalf("handoff error = %q", err)
	}
}

func TestLifecycleWaitBlockedStatusHonorsOperationDeadline(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "o115-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("ORBIT_HOME", home)
	if err := os.MkdirAll(daemon.OrbitDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", daemon.DefaultSocketPath())
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = waitForLifecycleJSONContext(ctx, daemon.NewClient(daemon.DefaultSocketPath()), []string{"worker"}, "healthy")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 1200*time.Millisecond {
		t.Fatalf("blocked status exceeded operation deadline: %s", elapsed)
	}
}

func TestEnvApplyProgrammaticCancellationWritesOneEnvelope(t *testing.T) {
	previousJSON := cli.JSONOutput
	previousStdout := os.Stdout
	t.Cleanup(func() {
		cli.JSONOutput = previousJSON
		os.Stdout = previousStdout
	})
	cli.JSONOutput = true
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writeEnd
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := envApplyCmd()
	cmd.SetContext(ctx)
	err = runEnvApply(cmd, nil)
	_ = writeEnd.Close()
	os.Stdout = previousStdout
	if err == nil {
		t.Fatal("runEnvApply() succeeded after cancellation")
	}
	output, readErr := io.ReadAll(readEnd)
	if readErr != nil {
		t.Fatal(readErr)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var envelope cli.JSONEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, output)
	}
	if envelope.Error == nil || envelope.Error.Code != "canceled" || envelope.SchemaVersion != cli.SchemaVersion {
		t.Fatalf("envelope = %+v", envelope)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contained more than one envelope: %v\n%s", err, output)
	}
}

func TestEnvApplyBlockedReconcileWritesOneTimeoutHandoffEnvelope(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "o115-apply-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("ORBIT_HOME", home)
	if err := os.MkdirAll(daemon.OrbitDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := daemon.WritePID(); err != nil {
		t.Fatal(err)
	}
	configFile = filepath.Join(home, "orbit.yaml")
	if err := os.WriteFile(configFile, []byte("version: \"3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	reconcileStarted := make(chan struct{})
	var reconcileOnce sync.Once
	listener, err := net.Listen("unix", daemon.DefaultSocketPath())
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			_ = json.NewEncoder(w).Encode(daemon.StatusResponse{ConfigStale: true})
		case "/api/env/reconcile":
			reconcileOnce.Do(func() { close(reconcileStarted) })
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		close(release)
		_ = server.Close()
	})

	previousJSON, previousStdout, previousStderr, previousTimeout := cli.JSONOutput, os.Stdout, os.Stderr, timeout
	t.Cleanup(func() {
		cli.JSONOutput = previousJSON
		os.Stdout = previousStdout
		os.Stderr = previousStderr
		timeout = previousTimeout
	})
	cli.JSONOutput = true
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writeEnd
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = stderrWrite
	cmd := envApplyCmd()
	timeout = 80 * time.Millisecond
	cmd.SetContext(context.Background())
	err = runEnvApply(cmd, nil)
	_ = writeEnd.Close()
	_ = stderrWrite.Close()
	os.Stdout = previousStdout
	os.Stderr = previousStderr
	if err == nil {
		t.Fatal("runEnvApply() succeeded after reconcile deadline")
	}
	output, readErr := io.ReadAll(readEnd)
	if readErr != nil {
		t.Fatal(readErr)
	}
	progressOutput, readErr := io.ReadAll(stderrRead)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got, want := string(progressOutput), "⋯ checking environment\n⋯ applying environment\n"; got != want {
		t.Fatalf("stderr progress = %q, want %q", got, want)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var envelope cli.JSONEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, output)
	}
	if envelope.Error == nil || envelope.Error.Code != "timeout" {
		t.Fatalf("envelope error = %+v", envelope.Error)
	}
	encodedData, _ := json.Marshal(envelope.Data)
	var data environmentApplyJSONData
	if err := json.Unmarshal(encodedData, &data); err != nil {
		t.Fatal(err)
	}
	if !data.ChangesPending || !data.DaemonRunning {
		t.Fatalf("partial data = %+v", data)
	}
	select {
	case <-reconcileStarted:
	default:
		t.Fatal("environment reconcile request was not dispatched")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contained more than one envelope: %v\n%s", err, output)
	}
}

func TestRunningEnvironmentResourcesPreservesActiveIntent(t *testing.T) {
	resources := []daemon.ResourceStatus{
		{Name: "stopped", State: "stopped"},
		{Name: "stopping", State: "stopping"},
		{Name: "healthy", State: "healthy"},
		{Name: "degraded", State: "degraded"},
		{Name: "starting", State: "starting"},
		{Name: "pending", State: "pending"},
		{Name: "building", State: "building"},
		{Name: "restarting", State: "restarting"},
	}

	got := runningEnvironmentResources(resources)
	want := []string{"building", "degraded", "healthy", "pending", "restarting", "starting"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runningEnvironmentResources() = %v, want %v", got, want)
	}
}

func TestRestorableEnvironmentResourcesSeparatesRemovedConfig(t *testing.T) {
	previouslyRunning := []string{"api", "database", "removed"}
	available := []daemon.ResourceStatus{
		{Name: "database", State: "stopped"},
		{Name: "api", State: "stopped"},
		{Name: "new", State: "stopped"},
	}

	restored, unavailable := restorableEnvironmentResources(previouslyRunning, available)
	if !reflect.DeepEqual(restored, []string{"api", "database"}) {
		t.Fatalf("restored = %v", restored)
	}
	if !reflect.DeepEqual(unavailable, []string{"removed"}) {
		t.Fatalf("unavailable = %v", unavailable)
	}
}

func TestEnvironmentApplySeparatesRestoredIntentFromNewDependencies(t *testing.T) {
	restored := []string{"api", "web"}
	affected := []string{"redis", "web", "api"}

	startedDependencies := daemonsrv.AdditionalResourceNames(restored, affected)
	if !reflect.DeepEqual(startedDependencies, []string{"redis"}) {
		t.Fatalf("started dependencies = %v", startedDependencies)
	}

	data := buildEnvironmentApplyJSONData(environmentApplyResult{
		Applied:             true,
		DaemonRunning:       true,
		PreviouslyRunning:   restored,
		RestoredResources:   restored,
		StartedDependencies: startedDependencies,
	})
	if !reflect.DeepEqual(data.RestoredResources, restored) ||
		!reflect.DeepEqual(data.StartedDependencies, []string{"redis"}) {
		t.Fatalf("apply data = %+v", data)
	}
}
