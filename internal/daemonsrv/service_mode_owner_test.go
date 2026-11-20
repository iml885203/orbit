package daemonsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
)

// This proposed regression is intentionally outside the checkout until the
// untouched baseline has been frozen. It exercises the real HTTP handler and
// orchestrator, replacing only external process/container boundaries.
func TestServiceModeStopsActiveOwnerBeforePublishingTarget(t *testing.T) {
	for _, scenario := range []struct{ from, target, targetKind string }{
		{"container", "dev", "service"}, {"service", "container", "container"},
	} {
		t.Run(scenario.from+"_to_"+scenario.target, func(t *testing.T) {
			cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}, Containers: map[string]*config.Container{"api": {Name: "api", Image: "api"}}}
			srv := newTestServer(t, cfg)
			orch := srv.app.Orchestrator
			orch.SetServiceKind("api", scenario.from)
			orch.MarkServiceHealthy("api")
			entered := make(chan string, 2)
			released := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(released) }) }
			defer unblock()
			stop := func(kind string) error { entered <- kind; <-released; return nil }
			orch.OnStopProcess = func(string) error { return stop("service") }
			orch.OnStopContainer = func(context.Context, string) error { return stop("container") }
			launches := make(chan string, 2)
			orch.OnStartProcess = func(context.Context, string, int, *config.Config, *config.Service) error {
				launches <- "service"
				return nil
			}
			orch.OnStartContainer = func(context.Context, string, int, *config.Container) error { launches <- "container"; return nil }
			orch.OnHealthCheck = nil
			ctx, cancel := context.WithCancel(context.Background())
			exited := make(chan error, 1)
			go func() { exited <- orch.Run(ctx) }()
			defer func() { cancel(); <-exited }()
			api := httptest.NewServer(http.HandlerFunc(srv.handleServiceMode))
			defer api.Close()
			request, err := http.NewRequest(http.MethodPut, api.URL+"/api/service-mode/api", strings.NewReader(`{"mode":"`+scenario.target+`"}`))
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Timeout: time.Second}
			response, err := client.Do(request)
			if err != nil {
				t.Fatalf("mode acceptance blocked: %v", err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status=%d", response.StatusCode)
			}
			select {
			case stopped := <-entered:
				if stopped != scenario.from {
					t.Errorf("stop dispatched to %s, active owner is %s", stopped, scenario.from)
				}
			case <-time.After(time.Second):
				t.Fatal("accepted transition did not start")
			}
			info, ok := orch.GetServiceInfo("api")
			if !ok || info.Kind != scenario.from {
				t.Errorf("target published before active owner retired: info=%+v", info)
			}
			snapshots := make(chan error, 1)
			go func() {
				status := srv.buildStatusResponse()
				for _, resource := range status.Resources {
					if resource.Name == "api" && string(resource.Kind) != scenario.from {
						snapshots <- fmt.Errorf("status kind=%s want %s", resource.Kind, scenario.from)
						return
					}
				}
				graphResponse := httptest.NewRecorder()
				srv.handleGraph(graphResponse, httptest.NewRequest(http.MethodGet, "/api/graph", nil))
				var graph GraphResponse
				if err := json.Unmarshal(graphResponse.Body.Bytes(), &graph); err != nil {
					snapshots <- err
					return
				}
				for _, node := range graph.Nodes {
					if node.Name == "api" && string(node.RuntimeKind) != scenario.from {
						snapshots <- fmt.Errorf("graph kind=%s want %s", node.RuntimeKind, scenario.from)
						return
					}
				}
				snapshots <- nil
			}()
			select {
			case err := <-snapshots:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(time.Second):
				t.Error("status/graph blocked behind mode retirement")
			}
			select {
			case launch := <-launches:
				t.Errorf("replacement launched before stop joined: %s", launch)
			default:
			}
			unblock()
			select {
			case launch := <-launches:
				if launch != scenario.targetKind {
					t.Errorf("launch=%s want %s", launch, scenario.targetKind)
				}
			case <-time.After(time.Second):
				t.Error("replacement did not launch")
			}
			srv.waitForBackground()
		})
	}
}

func TestServiceModeStopFailurePreservesActiveOwner(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}, Containers: map[string]*config.Container{"api": {Name: "api", Image: "api"}}}
	srv := newTestServer(t, cfg)
	orch := srv.app.Orchestrator
	orch.SetServiceKind("api", "container")
	orch.MarkServiceHealthy("api")
	sentinel := errors.New("owned resource could not stop")
	stopped := make(chan string, 2)
	orch.OnStopContainer = func(context.Context, string) error { stopped <- "container"; return sentinel }
	orch.OnStopProcess = func(string) error { stopped <- "service"; return sentinel }
	response := httptest.NewRecorder()
	srv.handleServiceMode(response, httptest.NewRequest(http.MethodPut, "/api/service-mode/api", strings.NewReader(`{"mode":"dev"}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("accept status=%d", response.Code)
	}
	srv.waitForBackground()
	select {
	case kind := <-stopped:
		if kind != "container" {
			t.Errorf("stopped=%s want container", kind)
		}
	default:
		t.Fatal("stop not attempted")
	}
	info, ok := orch.GetServiceInfo("api")
	if !ok || info.Kind != "container" || !strings.Contains(info.StateReason, sentinel.Error()) {
		t.Fatalf("failure lost active ownership/evidence: %+v", info)
	}
}

func TestServiceModeAdmissionAndResult(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprintf("running_%t", active), func(t *testing.T) {
			cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}, Containers: map[string]*config.Container{"api": {Name: "api", Image: "api"}}}
			srv := newTestServer(t, cfg)
			orch := srv.app.Orchestrator
			orch.MarkServiceHealthy("api")
			entered := make(chan struct{}, 1)
			gate := make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(gate) }) }
			defer release()
			orch.OnStopProcess = func(string) error { entered <- struct{}{}; <-gate; return errors.New("cannot retire owner") }
			// Hold the config writer to cover admission before execution, independently
			// of which background goroutine wins environmentTransitionMu.
			srv.configWriteMu.Lock()
			locked := true
			defer func() {
				if locked {
					srv.configWriteMu.Unlock()
				}
			}()
			request := func(mode string) *httptest.ResponseRecorder {
				result := httptest.NewRecorder()
				srv.handleServiceMode(result, httptest.NewRequest(http.MethodPut, "/api/service-mode/api", strings.NewReader(`{"mode":"`+mode+`"}`)))
				return result
			}
			accepted := request("container")
			if accepted.Code != http.StatusOK {
				t.Fatalf("accept=%d %s", accepted.Code, accepted.Body.String())
			}
			var ack APIResponse
			if err := json.Unmarshal(accepted.Body.Bytes(), &ack); err != nil {
				t.Fatal(err)
			}
			if ack.OperationID == "" {
				t.Fatal("accepted mode lacks observable operation")
			}
			if active {
				srv.configWriteMu.Unlock()
				locked = false
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("stop not entered")
				}
			}
			rejected := request("dev")
			if rejected.Code != http.StatusConflict || !strings.Contains(rejected.Body.String(), "operation_busy") {
				t.Fatalf("overlap=%d %s", rejected.Code, rejected.Body.String())
			}
			if srv.settings.GetServiceMode("api") != "container" {
				t.Fatal("rejected target persisted")
			}
			operation, ok := srv.lifecycle.read(ack.OperationID)
			if !ok || operation.TargetMode != "container" {
				t.Fatalf("captured target=%+v", operation)
			}
			if locked {
				srv.configWriteMu.Unlock()
				locked = false
			}
			release()
			srv.waitForBackground()
			result := httptest.NewRecorder()
			srv.handleLifecycleOperation(result, httptest.NewRequest(http.MethodGet, "/api/operations/"+ack.OperationID, nil))
			if err := json.Unmarshal(result.Body.Bytes(), &operation); err != nil {
				t.Fatal(err)
			}
			if operation.State != "failed" || operation.Code != "lifecycle_failed" {
				t.Fatalf("result=%+v", operation)
			}
			// The failed transition no longer reserves admission.
			orch.OnStopProcess = func(string) error { return nil }
			retried := request("dev")
			if retried.Code != http.StatusOK {
				t.Fatalf("terminal reservation retained: %s", retried.Body.String())
			}
			srv.waitForBackground()
		})
	}
}

func TestServiceModeAdmissionFailureDoesNotPersist(t *testing.T) {
	for _, reason := range []string{"stale", "capacity", "persistence"} {
		t.Run(reason, func(t *testing.T) {
			cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}, Containers: map[string]*config.Container{"api": {Name: "api", Image: "api"}}}
			srv := newTestServer(t, cfg)
			if reason == "capacity" {
				for i := 0; i < lifecycleActiveLimit; i++ {
					if _, err := srv.lifecycle.accept(daemon.LifecycleOperation{Kind: "stop"}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if reason == "persistence" {
				blocker := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
				srv.settings = LoadSettings(filepath.Join(blocker, "settings.json"))
			}
			request := httptest.NewRequest(http.MethodPut, "/api/service-mode/api", strings.NewReader(`{"mode":"container"}`))
			if reason == "stale" {
				request.Header.Set(daemon.ExpectedEpochHeader, "-1")
				request.Header.Set(daemon.ExpectedGenerationHeader, "0")
			}
			response := httptest.NewRecorder()
			srv.handleServiceMode(response, request)
			if response.Code == http.StatusOK {
				t.Fatalf("accepted %s failure", reason)
			}
			if srv.settings.GetServiceMode("api") != "dev" {
				t.Fatal("unaccepted settings persisted in memory")
			}
			info, _ := srv.app.Orchestrator.GetServiceInfo("api")
			if info.Kind != "service" {
				t.Fatalf("unaccepted runtime=%+v", info)
			}
			if reason == "persistence" {
				if _, err := srv.lifecycle.accept(daemon.LifecycleOperation{Kind: "mode", Resources: []string{"api"}}); err != nil {
					t.Fatalf("provisional reservation remains: %v", err)
				}
			}
		})
	}
}

func TestServiceModeEnvironmentSupersessionIsObservable(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}, Containers: map[string]*config.Container{"api": {Name: "api", Image: "api"}}}
	srv := newTestServer(t, cfg)
	srv.configWriteMu.Lock()
	response := httptest.NewRecorder()
	srv.handleServiceMode(response, httptest.NewRequest(http.MethodPut, "/api/service-mode/api", strings.NewReader(`{"mode":"container"}`)))
	var ack APIResponse
	if err := json.Unmarshal(response.Body.Bytes(), &ack); err != nil {
		srv.configWriteMu.Unlock()
		t.Fatal(err)
	}
	srv.environmentGeneration.Add(1)
	srv.configWriteMu.Unlock()
	srv.waitForBackground()
	operation, ok := srv.lifecycle.read(ack.OperationID)
	if !ok || operation.State != "failed" || operation.Code != "operation_superseded" {
		t.Fatalf("supersession=%+v", operation)
	}
	info, _ := srv.app.Orchestrator.GetServiceInfo("api")
	if info.Kind != "service" || info.State.String() != "stopped" {
		t.Fatalf("stale operation touched replacement: %+v", info)
	}
}

func TestServiceModeCurrentTargetStillRestarts(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}, Containers: map[string]*config.Container{"api": {Name: "api", Image: "api"}}}
	srv := newTestServer(t, cfg)
	stopped := 0
	srv.app.Orchestrator.OnStopProcess = func(string) error { stopped++; return nil }
	response := httptest.NewRecorder()
	srv.handleServiceMode(response, httptest.NewRequest(http.MethodPut, "/api/service-mode/api", strings.NewReader(`{"mode":"dev"}`)))
	srv.waitForBackground()
	info, _ := srv.app.Orchestrator.GetServiceInfo("api")
	if response.Code != http.StatusOK || stopped != 1 || info.RestartCount != 1 {
		t.Fatalf("same-target stopped=%d info=%+v", stopped, info)
	}
}

func TestServiceModeReservedBeforeWorkerRejectsWithoutSaving(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}, Containers: map[string]*config.Container{"api": {Name: "api", Image: "api"}}}
	srv := newTestServer(t, cfg)
	operation, err := srv.lifecycle.accept(daemon.LifecycleOperation{Kind: "mode", TargetMode: "container", Resources: []string{"api"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"dev", "container"} {
		response := httptest.NewRecorder()
		srv.handleServiceMode(response, httptest.NewRequest(http.MethodPut, "/api/service-mode/api", strings.NewReader(`{"mode":"`+target+`"}`)))
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "operation_busy") {
			t.Fatalf("reserved target=%s response=%d %s", target, response.Code, response.Body.String())
		}
		if srv.settings.GetServiceMode("api") != "dev" {
			t.Fatal("reserved rejected target saved")
		}
	}
	captured, _ := srv.lifecycle.read(operation.ID)
	if captured.TargetMode != "container" || captured.State != "accepted" {
		t.Fatalf("pending intent changed: %+v", captured)
	}
}

func TestServiceModeFullShutdownGraceStillDispatchesReplacement(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node"}}, Containers: map[string]*config.Container{"api": {Name: "api", Image: "api"}}}
	cfg.Settings.ShutdownTimeout = 20 * time.Millisecond
	srv := newTestServer(t, cfg)
	orch := srv.app.Orchestrator
	orch.MarkServiceHealthy("api")
	retired := false
	orch.OnStopProcess = func(string) error {
		// Successful stop may consume all group grace before reaping and final
		// output draining. This is still success, not operation cancellation.
		timer := time.NewTimer(cfg.Settings.ShutdownTimeout + 20*time.Millisecond)
		defer timer.Stop()
		<-timer.C
		retired = true
		return nil
	}
	launched := make(chan bool, 1)
	orch.OnStartContainer = func(context.Context, string, int, *config.Container) error { launched <- retired; return nil }
	orch.OnHealthCheck = nil
	ctx, cancel := context.WithCancel(context.Background())
	exited := make(chan error, 1)
	go func() { exited <- orch.Run(ctx) }()
	defer func() { cancel(); <-exited }()
	api := httptest.NewServer(http.HandlerFunc(srv.handleServiceMode))
	defer api.Close()
	request, err := http.NewRequest(http.MethodPut, api.URL+"/api/service-mode/api", strings.NewReader(`{"mode":"container"}`))
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Timeout: time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	var ack APIResponse
	if err := json.NewDecoder(response.Body).Decode(&ack); err != nil {
		t.Fatal(err)
	}
	srv.waitForBackground()
	operation, ok := srv.lifecycle.read(ack.OperationID)
	if !ok || operation.State != "succeeded" {
		t.Fatalf("successful full-grace stop lost replacement: %+v", operation)
	}
	select {
	case joined := <-launched:
		if !joined {
			t.Error("replacement preceded retirement")
		}
	case <-time.After(time.Second):
		t.Error("successful full-grace stop did not dispatch replacement")
	}
}
