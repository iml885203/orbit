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
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/daemonsrv"
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
	home, err := os.MkdirTemp(shortTestTempRoot(), "o115-")
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
	home, err := os.MkdirTemp(shortTestTempRoot(), "o115-apply-")
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

	previousJSON, previousStdout, previousStderr := cli.JSONOutput, os.Stdout, os.Stderr
	t.Cleanup(func() {
		cli.JSONOutput = previousJSON
		os.Stdout = previousStdout
		os.Stderr = previousStderr
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
	ctx := &envApplyControlledDeadline{done: make(chan struct{}), deadline: time.Now().Add(time.Minute)}
	ticks := make(chan time.Time, 1)
	heartbeat := &envApplyHeartbeatWriter{Writer: os.Stderr, observed: make(chan struct{})}
	progress := newLifecycleProgressWithTicker(ctx, heartbeat, time.Now(), 20*time.Millisecond, ticks, func() {})
	result := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result <- runEnvApplyJSON(ctx, progress)
	}()
	defer func() {
		ctx.expire()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("environment apply did not exit after its deadline")
		}
	}()
	select {
	case <-reconcileStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("environment reconcile request was not dispatched")
	}
	ticks <- time.Now().Add(20 * time.Millisecond)
	select {
	case <-heartbeat.observed:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked reconcile did not emit a heartbeat")
	}
	ctx.expire()
	select {
	case err = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("environment apply did not report its deadline")
	}
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
	lines := strings.Split(strings.TrimSpace(string(progressOutput)), "\n")
	if got := string(progressOutput); len(lines) != 3 || lines[0] != "⋯ checking environment" || lines[1] != "⋯ applying environment" || !strings.HasPrefix(lines[2], "⋯ applying environment (elapsed ") || !strings.HasSuffix(lines[2], " remaining)") {
		t.Fatalf("stderr progress did not heartbeat during blocked reconcile: %q", got)
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

type envApplyControlledDeadline struct {
	done     chan struct{}
	deadline time.Time
	once     sync.Once
}

func (c *envApplyControlledDeadline) Deadline() (time.Time, bool) { return c.deadline, true }
func (c *envApplyControlledDeadline) Done() <-chan struct{}       { return c.done }
func (c *envApplyControlledDeadline) Value(any) any               { return nil }
func (c *envApplyControlledDeadline) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}
func (c *envApplyControlledDeadline) expire() { c.once.Do(func() { close(c.done) }) }

type envApplyHeartbeatWriter struct {
	io.Writer
	observed chan struct{}
	once     sync.Once
}

func (w *envApplyHeartbeatWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err == nil && bytes.HasPrefix(p, []byte("⋯ applying environment (elapsed ")) {
		w.once.Do(func() { close(w.observed) })
	}
	return n, err
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

// After a source sync, a running daemon with stale config is offered an
// apply. --dry never asks the daemon, and --no-apply defers with the command
// to run later instead of applying.
func TestSourceSyncApplyOfferHonorsDryAndNoApply(t *testing.T) {
	home, err := os.MkdirTemp(shortTestTempRoot(), "apply-offer-")
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
	listener, err := net.Listen("unix", daemon.DefaultSocketPath())
	if err != nil {
		t.Fatal(err)
	}
	var statusCalls, reconcileCalls atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			statusCalls.Add(1)
			_ = json.NewEncoder(w).Encode(daemon.StatusResponse{ConfigStale: true})
		case "/api/env/reconcile":
			reconcileCalls.Add(1)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	finishCapture := captureLifecycleProcessStreams(t)
	if err := offerEnvironmentApply(sourceSyncOptions{dry: true}); err != nil {
		t.Fatal(err)
	}
	if err := offerEnvironmentApply(sourceSyncOptions{noApply: true}); err != nil {
		t.Fatal(err)
	}
	output, _ := finishCapture()

	if got := statusCalls.Load(); got != 1 {
		t.Fatalf("status calls = %d, want 1: --dry must not ask the daemon", got)
	}
	if got := reconcileCalls.Load(); got != 0 {
		t.Fatalf("reconcile calls = %d, want 0: neither --dry nor --no-apply applies", got)
	}
	if !strings.Contains(string(output), "Apply later with: orbit env apply") {
		t.Fatalf("output = %q, want the deferred apply command", output)
	}
}
