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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

func TestUpJSONNoAffectedResourcesSkipsReadinessPhaseAndWritesOneEnvelope(t *testing.T) {
	prepareLifecycleProgressTest(t)
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/status":
			daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{ConfigPath: configFile})
		case "/api/up":
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, Message: "already healthy"})
		default:
			http.NotFound(w, r)
		}
	})
	restore := captureLifecycleProcessStreams(t)
	defer restore()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := runUpJSONWithProgress(ctx, nil, nil, newLifecycleProgress(ctx, os.Stderr, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	out, diagnostics := restore()
	assertOneLifecycleEnvelope(t, out, true)
	if strings.Contains(diagnostics, "waiting for readiness") {
		t.Fatalf("no-op up announced a skipped readiness wait: %q", diagnostics)
	}
	if got, want := diagnostics, "⋯ ensuring daemon\n⋯ checking environment\n⋯ requesting resource start\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestUpJSONFailureReportsEvidencePhaseOnceAndWritesOneEnvelope(t *testing.T) {
	prepareLifecycleProgressTest(t)
	var started atomic.Bool
	var logRequests atomic.Int32
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/health":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/status":
			status := daemon.StatusResponse{ConfigPath: configFile}
			if started.Load() {
				status.Resources = []daemon.ResourceStatus{{Name: "api", State: "degraded", StateReason: "exited"}}
			}
			daemon.WriteJSON(w, http.StatusOK, status)
		case r.URL.Path == "/api/up":
			started.Store(true)
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, AffectedResources: []string{"api"}})
		case strings.HasPrefix(r.URL.Path, "/api/logs/api"):
			logRequests.Add(1)
			daemon.WriteJSON(w, http.StatusOK, daemon.LogsResponse{Lines: []string{"application failed"}})
		default:
			http.NotFound(w, r)
		}
	})
	restore := captureLifecycleProcessStreams(t)
	defer restore()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := runUpJSONWithProgress(ctx, nil, nil, newLifecycleProgress(ctx, os.Stderr, time.Now()))
	if err == nil {
		t.Fatal("up succeeded despite degraded resource")
	}
	out, diagnostics := restore()
	assertOneLifecycleEnvelope(t, out, false)
	if count := strings.Count(diagnostics, "⋯ collecting failure evidence\n"); count != 1 {
		t.Fatalf("evidence phase count = %d in %q", count, diagnostics)
	}
	if got := logRequests.Load(); got != 2 {
		t.Fatalf("log evidence requests = %d, want terminal evidence plus final tail", got)
	}
}

func TestUpJSONReadinessFeedsResourceHeartbeatIntoSharedReporter(t *testing.T) {
	prepareLifecycleProgressTest(t)
	var started atomic.Bool
	var statusAfterStart atomic.Int32
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/status":
			status := daemon.StatusResponse{ConfigPath: configFile}
			if started.Load() {
				state := "starting"
				if statusAfterStart.Add(1) > 1 {
					state = "healthy"
				}
				status.Resources = []daemon.ResourceStatus{{Name: "api", State: state}}
			}
			daemon.WriteJSON(w, http.StatusOK, status)
		case "/api/up":
			started.Store(true)
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, AffectedResources: []string{"api"}})
		default:
			http.NotFound(w, r)
		}
	})
	restore := captureLifecycleProcessStreams(t)
	defer restore()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	progress := newLifecycleProgressWithTicker(ctx, os.Stderr, time.Now(), 40*time.Millisecond, ticker.C, ticker.Stop)
	err := runUpJSONWithProgress(ctx, nil, nil, progress)
	if err != nil {
		t.Fatal(err)
	}
	out, diagnostics := restore()
	assertOneLifecycleEnvelope(t, out, true)
	if !strings.Contains(diagnostics, "⋯ api still starting (elapsed 0s, about 1s remaining)\n") {
		t.Fatalf("resource heartbeat did not reach stderr: %q", diagnostics)
	}
}

func prepareLifecycleProgressTest(t *testing.T) {
	t.Helper()
	home, err := os.MkdirTemp(shortTestTempRoot(), "o119-")
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
	previousJSON, previousConfig, previousGroups, previousInfra := cli.JSONOutput, configFile, groups, infraOnly
	cli.JSONOutput = true
	configFile = filepath.Join(home, "orbit.yaml")
	groups = nil
	infraOnly = false
	t.Cleanup(func() {
		cli.JSONOutput = previousJSON
		configFile = previousConfig
		groups = previousGroups
		infraOnly = previousInfra
	})
}

func serveLifecycleProgressDaemon(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	listener, err := net.Listen("unix", daemon.DefaultSocketPath())
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
}

func captureLifecycleProcessStreams(t *testing.T) func() ([]byte, string) {
	t.Helper()
	previousStdout, previousStderr := os.Stdout, os.Stderr
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = stdoutWrite, stderrWrite
	var restored atomic.Bool
	return func() ([]byte, string) {
		if !restored.CompareAndSwap(false, true) {
			return nil, ""
		}
		_ = stdoutWrite.Close()
		_ = stderrWrite.Close()
		os.Stdout, os.Stderr = previousStdout, previousStderr
		out, readErr := io.ReadAll(stdoutRead)
		if readErr != nil {
			t.Fatal(readErr)
		}
		diagnostics, readErr := io.ReadAll(stderrRead)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return out, string(diagnostics)
	}
}

func assertOneLifecycleEnvelope(t *testing.T, output []byte, wantOK bool) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(output))
	var envelope cli.JSONEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, output)
	}
	if envelope.SchemaVersion != cli.SchemaVersion || envelope.OK != wantOK {
		t.Fatalf("envelope = %+v, want ok=%v", envelope, wantOK)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contained more than one envelope: %v\n%s", err, output)
	}
}
