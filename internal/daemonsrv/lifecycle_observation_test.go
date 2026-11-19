package daemonsrv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsource"
	"github.com/iml885203/orbit/internal/extension"
)

func TestLifecycleStopKeepsStatusAndSSEReadable(t *testing.T) {
	for _, kind := range []string{"stop", "restart", "down"} {
		t.Run(kind, func(t *testing.T) {
			cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}, Containers: map[string]*config.Container{}}
			srv := newTestServer(t, cfg)
			srv.app.Orchestrator.MarkServiceHealthy("api")
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			srv.app.Orchestrator.OnStopProcess = func(string) error { close(entered); <-release; return nil }
			ack := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/"+kind+"/api", nil)
			switch kind {
			case "down":
				srv.handleDown(ack, request)
			case "restart":
				srv.handleRestart(ack, request)
			default:
				srv.handleStop(ack, request)
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("stop not started")
			}
			snapshots := make(chan StatusResponse, 1)
			go func() {
				srv.handleStatus(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/status", nil))
				snapshots <- srv.buildStatusResponse()
			}()
			select {
			case snapshot := <-snapshots:
				if len(snapshot.Resources) != 1 || snapshot.Resources[0].State != "stopping" {
					t.Fatalf("snapshot=%+v", snapshot)
				}
			case <-time.After(time.Second):
				t.Fatal("status/SSE blocked by service shutdown")
			}
			unblock()
			srv.waitForBackground()
		})
	}
}

func newLifecycleDockerServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
	if cfg.Settings.ShutdownTimeout == 0 {
		cfg.Settings.ShutdownTimeout = time.Second
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("API-Version", "1.52")
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			_, _ = w.Write([]byte("OK"))
		case strings.HasSuffix(r.URL.Path, "/containers/json"), strings.HasSuffix(r.URL.Path, "/networks"):
			_, _ = w.Write([]byte("[]"))
		case strings.HasSuffix(r.URL.Path, "/networks/create"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"test"}`))
		case strings.HasSuffix(r.URL.Path, "/stop"), r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		}
	}))
	t.Cleanup(api.Close)
	t.Setenv("DOCKER_API_VERSION", "1.52")
	return newTestServerAtDockerHost(t, cfg, "tcp://"+strings.TrimPrefix(api.URL, "http://"))
}

func TestStopOperationReportsFailureWithoutObservingIntermediateState(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}}
	srv := newTestServer(t, cfg)
	srv.app.Orchestrator.MarkServiceHealthy("api")
	srv.app.Orchestrator.OnStopProcess = func(string) error { return errors.New("controlled stop failure") }
	ack := httptest.NewRecorder()
	srv.handleStop(ack, httptest.NewRequest(http.MethodPost, "/api/stop/api", nil))
	var accepted APIResponse
	if err := json.Unmarshal(ack.Body.Bytes(), &accepted); err != nil || accepted.OperationID == "" {
		t.Fatalf("ack=%s err=%v", ack.Body.String(), err)
	}
	srv.waitForBackground()
	result := httptest.NewRecorder()
	srv.handleLifecycleOperation(result, httptest.NewRequest(http.MethodGet, "/api/operations/"+accepted.OperationID, nil))
	var operation daemon.LifecycleOperation
	if err := json.Unmarshal(result.Body.Bytes(), &operation); err != nil {
		t.Fatal(err)
	}
	if operation.State != "failed" || operation.Code != "lifecycle_failed" || !strings.Contains(operation.Error, "controlled stop failure") || operation.FinishedAt == nil {
		t.Fatalf("operation=%+v", operation)
	}
	if state := srv.buildStatusResponse().Resources[0].State; state != "degraded" {
		t.Fatalf("state=%s", state)
	}
}

func TestDownOperationReportsSweepFailureAfterTrackedResourcesStop(t *testing.T) {
	cfg := &config.Config{Containers: map[string]*config.Container{"db": {Name: "db", Image: "postgres:17"}}}
	cfg.Settings.ShutdownTimeout = time.Second
	srv := newTestServer(t, cfg)
	srv.app.Orchestrator.OnStopContainer = func(_ context.Context, _ string) error { return nil }
	ack := httptest.NewRecorder()
	srv.handleDown(ack, httptest.NewRequest(http.MethodPost, "/api/down", nil))
	var accepted APIResponse
	if err := json.Unmarshal(ack.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	srv.waitForBackground()
	operation, ok := srv.lifecycle.read(accepted.OperationID)
	if !ok || operation.State != "failed" || operation.Cleanup != "failed" || !strings.Contains(operation.Error, "container sweep") {
		t.Fatalf("operation=%+v", operation)
	}
	if state := srv.buildStatusResponse().Resources[0].State; state != "stopped" {
		t.Fatalf("state=%s", state)
	}
}

func TestHostOnlyDownDoesNotRequireDocker(t *testing.T) {
	srv := newTestServer(t, &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}})
	srv.app.Orchestrator.OnStopProcess = func(string) error { return nil }
	ack := httptest.NewRecorder()
	srv.handleDown(ack, httptest.NewRequest(http.MethodPost, "/api/down", nil))
	var accepted APIResponse
	if err := json.Unmarshal(ack.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	srv.waitForBackground()
	operation, _ := srv.lifecycle.read(accepted.OperationID)
	if operation.State != "succeeded" || operation.Cleanup != "unavailable" || len(operation.Warnings) != 1 {
		t.Fatalf("operation=%+v", operation)
	}
}

func TestSynchronousDownPreservesUnverifiedCleanup(t *testing.T) {
	srv := newTestServer(t, &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}})
	srv.app.Orchestrator.OnStopProcess = func(string) error { return nil }
	out := httptest.NewRecorder()
	srv.handleDown(out, httptest.NewRequest(http.MethodPost, "/api/down", strings.NewReader(`{"wait":true}`)))
	var result APIResponse
	if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if out.Code != http.StatusOK || !result.OK || result.Cleanup != "unavailable" || len(result.Warnings) != 1 {
		t.Fatalf("status=%d result=%+v", out.Code, result)
	}
	if result.Message != "stopped configured resources" || strings.Contains(result.Warnings[0], "resources stopped") {
		t.Fatalf("unverified cleanup overclaimed: %+v", result)
	}
}

func TestQueuedOperationRejectsChangedGeneration(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	srv.environmentTransitionMu.Lock()
	ran := false
	operation, err := srv.acceptLifecycle(nil, "stop", []string{"api"}, func(string) error { ran = true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	srv.SetConfigPath(filepath.Join(t.TempDir(), "changed.yaml"))
	srv.environmentTransitionMu.Unlock()
	srv.waitForBackground()
	result, _ := srv.lifecycle.read(operation.ID)
	if ran || result.State != "failed" || result.Code != "operation_superseded" {
		t.Fatalf("ran=%t result=%+v", ran, result)
	}
}

func TestLifecycleOperationRetentionNeverEvictsAcceptedWork(t *testing.T) {
	var store lifecycleOperations
	active, err := store.accept(daemon.LifecycleOperation{Kind: "stop"})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < lifecycleOperationLimit+20; index++ {
		operation, err := store.accept(daemon.LifecycleOperation{Kind: "stop"})
		if err != nil {
			t.Fatal(err)
		}
		store.finish(operation.ID, "", nil)
	}
	if _, ok := store.read(active.ID); !ok {
		t.Fatal("accepted operation evicted")
	}
	if len(store.records) > lifecycleOperationLimit {
		t.Fatal("terminal retention unbounded")
	}
	for index := 1; index < lifecycleActiveLimit; index++ {
		if _, err := store.accept(daemon.LifecycleOperation{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.accept(daemon.LifecycleOperation{}); !errors.Is(err, errLifecycleCapacity) {
		t.Fatalf("capacity=%v", err)
	}
	if _, ok := store.read(active.ID); !ok {
		t.Fatal("active operation lost at capacity")
	}
}

func TestConfigurationPreparationDoesNotBlockSnapshotsAndPublishesAtomically(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.yaml")
	newPath := filepath.Join(dir, "new.yaml")
	for _, path := range []string{oldPath, newPath} {
		if err := os.WriteFile(path, []byte("version: \"3\"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	srv := newTestServer(t, &config.Config{Version: "old"})
	srv.SetConfigPath(oldPath)
	generation := srv.environmentGeneration.Load()
	staged := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- srv.UpdateConfig(func(tx extension.ConfigTx) error {
			tx.Store(&config.Config{Version: "new"})
			tx.SetConfigPath(newPath)
			tx.MarkEngineStale()
			close(staged)
			<-release
			return nil
		})
	}()
	<-staged
	observed := make(chan StatusResponse, 1)
	go func() { observed <- srv.buildStatusResponse() }()
	var snapshot StatusResponse
	select {
	case snapshot = <-observed:
	case <-time.After(time.Second):
		close(release)
		<-done
		t.Fatal("configuration preparation blocked observation")
	}
	if snapshot.ConfigPath != oldPath || snapshot.Generation != generation || srv.holder.Load().Version != "old" {
		close(release)
		<-done
		t.Fatalf("partial publication: %+v", snapshot)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	snapshot = srv.buildStatusResponse()
	if snapshot.ConfigPath != newPath || snapshot.Generation <= generation || srv.holder.Load().Version != "new" || !snapshot.ConfigStale {
		t.Fatalf("publication=%+v", snapshot)
	}
}

func TestSerializedExternalWorkDoesNotBlockSnapshots(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	srv.environmentTransitionMu.Lock()
	done := make(chan struct{})
	go func() {
		srv.handleStatus(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/status", nil))
		srv.buildStatusResponse()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		srv.environmentTransitionMu.Unlock()
		t.Fatal("read waited on transition serialization")
	}
	srv.environmentTransitionMu.Unlock()
}

func TestLifecycleMutationRejectsStaleEnvironmentBeforeAcceptance(t *testing.T) {
	srv := newTestServer(t, &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}})
	ran := false
	srv.app.Orchestrator.OnStopProcess = func(string) error { ran = true; return nil }
	request := httptest.NewRequest(http.MethodPost, "/api/stop/api", nil)
	request.Header.Set(daemon.ExpectedEpochHeader, "1")
	request.Header.Set(daemon.ExpectedGenerationHeader, "0")
	result := httptest.NewRecorder()
	srv.handleStop(result, request)
	srv.waitForBackground()
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "environment_changed") || ran || len(srv.lifecycle.records) != 0 {
		t.Fatalf("code=%d body=%s ran=%t", result.Code, result.Body.String(), ran)
	}
}

func TestCancelObservationDoesNotCancelAcceptedStop(t *testing.T) {
	srv := newTestServer(t, &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}})
	entered := make(chan struct{})
	release := make(chan struct{})
	srv.app.Orchestrator.OnStopProcess = func(string) error { close(entered); <-release; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/stop/api", nil).WithContext(ctx)
	ack := httptest.NewRecorder()
	srv.handleStop(ack, request)
	var accepted APIResponse
	if err := json.Unmarshal(ack.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	<-entered
	cancel()
	operation, ok := srv.lifecycle.read(accepted.OperationID)
	if !ok || operation.State != "running" {
		close(release)
		t.Fatalf("operation=%+v", operation)
	}
	close(release)
	srv.waitForBackground()
	operation, _ = srv.lifecycle.read(accepted.OperationID)
	if operation.State != "succeeded" {
		t.Fatalf("observation cancelled accepted work: %+v", operation)
	}
}

func TestReconcilePublishesRuntimeAndConfigInOneGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	if err := os.WriteFile(path, []byte("version: \"3\"\nservices:\n  api:\n    command: api\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, cfg)
	srv.SetConfigPath(path)
	srv.app.Orchestrator.MarkServiceHealthy("api")
	generation := srv.environmentGeneration.Load()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	srv.app.Orchestrator.OnStopProcess = func(string) error { close(entered); <-release; return nil }
	if err := os.WriteFile(path, []byte("version: \"3\"\nservices:\n  worker:\n    command: worker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	response := httptest.NewRecorder()
	go func() {
		srv.handleEnvironmentReconcile(response, httptest.NewRequest(http.MethodPost, "/api/env/reconcile", nil))
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reconcile did not enter stop")
	}
	observed := make(chan StatusResponse, 1)
	go func() { observed <- srv.buildStatusResponse() }()
	select {
	case snapshot := <-observed:
		if snapshot.Generation != generation || len(snapshot.Resources) != 1 || snapshot.Resources[0].Name != "api" || snapshot.Resources[0].State != "stopping" {
			t.Fatalf("old generation=%+v", snapshot)
		}
	case <-time.After(time.Second):
		t.Fatal("reconcile blocked status")
	}
	unblock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconcile did not finish")
	}
	snapshot := srv.buildStatusResponse()
	if response.Code != http.StatusOK || snapshot.Generation <= generation || len(snapshot.Resources) != 1 || snapshot.Resources[0].Name != "worker" {
		t.Fatalf("new generation=%+v response=%s", snapshot, response.Body.String())
	}
}

func TestSourceRefreshKeepsStatusAndSSEReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Git transport uses a POSIX shell")
	}
	srv := newTestServer(t, &config.Config{})
	bin := t.TempDir()
	entered := filepath.Join(bin, "entered")
	released := filepath.Join(bin, "release")
	t.Setenv("ORBIT_TEST_GIT_ENTERED", entered)
	t.Setenv("ORBIT_TEST_GIT_RELEASE", released)
	script := `#!/bin/sh
case "$1" in
 clone)
  : > "$ORBIT_TEST_GIT_ENTERED"
  while [ ! -f "$ORBIT_TEST_GIT_RELEASE" ]; do sleep 0.01; done
  for arg do destination="$arg"; done
  mkdir -p "$destination/envs"
  printf 'version: "3"\n' > "$destination/envs/dev.yaml"
  ;;
 rev-parse) printf '0123456789abcdef\n' ;;
 symbolic-ref) printf 'main\n' ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	registry, err := envsource.Load(envsource.RegistryPath(OrbitDir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "team", Type: envsource.TypeGit, URL: "https://example.invalid/team.git"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	response := httptest.NewRecorder()
	go func() {
		srv.handleSources(response, httptest.NewRequest(http.MethodPost, "/api/sources", strings.NewReader(`{"action":"sync","name":"team"}`)))
		close(done)
	}()
	defer func() {
		_ = os.WriteFile(released, nil, 0600)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("source refresh did not finish")
		}
	}()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
waiting:
	for {
		select {
		case <-deadline.C:
			t.Fatal("source transport did not start")
		case <-ticker.C:
			if _, err := os.Stat(entered); err == nil {
				break waiting
			}
		}
	}
	read := make(chan StatusResponse, 1)
	go func() {
		srv.handleStatus(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/status", nil))
		read <- srv.buildStatusResponse()
	}()
	select {
	case snapshot := <-read:
		if snapshot.Epoch != srv.epoch() {
			t.Fatalf("epoch=%d", snapshot.Epoch)
		}
	case <-time.After(time.Second):
		t.Fatal("source refresh blocked read-only observation")
	}
	if err := os.WriteFile(released, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if response.Code != http.StatusOK {
			t.Fatalf("source response=%s", response.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("source refresh did not complete")
	}
}

func TestAcceptedOperationWaitsForConfigWriterBeforeGenerationCheck(t *testing.T) {
	oldPath := filepath.Join(t.TempDir(), "old.yaml")
	newPath := filepath.Join(t.TempDir(), "new.yaml")
	srv := newTestServer(t, &config.Config{})
	srv.SetConfigPath(oldPath)
	entered := make(chan struct{})
	release := make(chan struct{})
	written := make(chan error, 1)
	go func() {
		written <- srv.UpdateConfig(func(tx extension.ConfigTx) error {
			tx.Store(&config.Config{Version: "new"})
			tx.SetConfigPath(newPath)
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	ran := false
	operation, err := srv.acceptLifecycle(nil, "stop", []string{"api"}, func(string) error { ran = true; return nil })
	if err != nil {
		close(release)
		<-written
		t.Fatal(err)
	}
	close(release)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	srv.waitForBackground()
	result, _ := srv.lifecycle.read(operation.ID)
	if ran || result.State != "failed" || result.Code != "operation_superseded" {
		t.Fatalf("ran=%t operation=%+v", ran, result)
	}
}

func TestRuntimeModePublicationIgnoresStagedSettingsIntent(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}, Containers: map[string]*config.Container{"api": {Name: "api", Image: "api"}}}
	srv := newTestServer(t, cfg)
	srv.app.Orchestrator.SetServiceKind("api", "service")
	if err := srv.settings.SetServiceMode("api", "container"); err != nil {
		t.Fatal(err)
	}
	check := func(kind ResourceKind, mode string) {
		t.Helper()
		for _, resource := range srv.buildStatusResponse().Resources {
			if resource.Name == "api" && (resource.Kind != kind || resource.Mode != mode) {
				t.Fatalf("SSE resource=%+v", resource)
			}
		}
		status := httptest.NewRecorder()
		srv.handleStatus(status, httptest.NewRequest(http.MethodGet, "/api/status", nil))
		var snapshot StatusResponse
		if err := json.Unmarshal(status.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		for _, resource := range snapshot.Resources {
			if resource.Name == "api" && (resource.Kind != kind || resource.Mode != mode) {
				t.Fatalf("status resource=%+v", resource)
			}
		}
		graph := httptest.NewRecorder()
		srv.handleGraph(graph, httptest.NewRequest(http.MethodGet, "/api/graph", nil))
		var view GraphResponse
		if err := json.Unmarshal(graph.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		for _, node := range view.Nodes {
			if node.Name == "api" && node.Mode != mode {
				t.Fatalf("graph node=%+v", node)
			}
		}
	}
	check(ResourceKindService, "dev")
	srv.snapshotMu.Lock()
	srv.app.Orchestrator.SetServiceKind("api", "container")
	srv.snapshotMu.Unlock()
	check(ResourceKindContainer, "container")
}

func TestHostOnlyDownSweepsPreviousConfigurationOrphans(t *testing.T) {
	for _, scenario := range []string{"success", "denied", "lost_after_list"} {
		t.Run(scenario, func(t *testing.T) {
			var stopped, removed bool
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("API-Version", "1.52")
				switch {
				case strings.HasSuffix(r.URL.Path, "/_ping"):
					_, _ = w.Write([]byte("OK"))
				case strings.HasSuffix(r.URL.Path, "/containers/json"):
					if scenario == "denied" {
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte(`{"message":"denied"}`))
						return
					}
					_, _ = w.Write([]byte(`[{"Id":"orphan","Labels":{"orbit.managed":"true","orbit.namespace":"orbit-test"}}]`))
				case strings.HasSuffix(r.URL.Path, "/networks"):
					_, _ = w.Write([]byte(`[]`))
				case strings.HasSuffix(r.URL.Path, "/networks/create"):
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"Id":"test"}`))
				case strings.HasSuffix(r.URL.Path, "/orphan/stop"):
					if scenario == "lost_after_list" {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err == nil {
							_ = conn.Close()
						}
						return
					}
					stopped = true
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodDelete:
					removed = true
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer api.Close()
			t.Setenv("DOCKER_API_VERSION", "1.52")
			srv := newTestServerAtDockerHost(t, &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}}, "tcp://"+strings.TrimPrefix(api.URL, "http://"))
			ack := httptest.NewRecorder()
			srv.handleDown(ack, httptest.NewRequest(http.MethodPost, "/api/down", nil))
			var accepted APIResponse
			if err := json.Unmarshal(ack.Body.Bytes(), &accepted); err != nil {
				t.Fatal(err)
			}
			srv.waitForBackground()
			operation, _ := srv.lifecycle.read(accepted.OperationID)
			if scenario != "success" {
				if operation.State != "failed" || operation.Cleanup != "failed" || len(operation.Warnings) != 0 {
					t.Fatalf("operation=%+v", operation)
				}
			} else if operation.State != "succeeded" || operation.Cleanup != "succeeded" || !stopped || !removed {
				t.Fatalf("operation=%+v stopped=%v removed=%v namespace=%s", operation, stopped, removed, srv.app.ContainerMgr.Namespace())
			}
		})
	}
}
