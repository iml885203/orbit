package daemonsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/platform"
)

func TestModePreparationProcessHelper(t *testing.T) {
	if os.Getenv("ORBIT_MODE_PREPARATION_HELPER") != "1" {
		return
	}
	stage := os.Args[len(os.Args)-1]
	if stage == "main" {
		fmt.Println("UNEXPECTED-MAIN")
		return
	}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stopped)
	if stage == "child" {
		fmt.Printf("CHILD-READY=%d\n", os.Getpid())
		<-stopped
		return
	}
	child := exec.Command(os.Args[0], "-test.run=^TestModePreparationProcessHelper$", "-test.timeout=15s", "--", "child")
	child.Stdout, child.Stderr, child.Env = os.Stdout, os.Stderr, os.Environ()
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	<-stopped
	// The group receives the same stop. Reap the child before this leader exits,
	// so child liveness at replacement is not confused with orphan zombies.
	_ = child.Wait()
}

func TestServiceModeRetiresOwnedPreparationBeforeTargetStart(t *testing.T) {
	command := func(stage string) string {
		return strconv.Quote(filepath.ToSlash(os.Args[0])) + " -test.run=^TestModePreparationProcessHelper$ -test.timeout=15s -- " + stage
	}
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node", Path: t.TempDir(), Command: command("main"), PreStart: []string{command("tree")}, Env: map[string]string{"ORBIT_MODE_PREPARATION_HELPER": "1"}}}, Containers: map[string]*config.Container{"api": {Name: "api", Image: "api"}}}
	cfg.Settings.ShutdownTimeout = time.Second
	srv := newTestServer(t, cfg)
	manager := srv.app.ProcessMgr
	orch := srv.app.Orchestrator
	orch.OnHealthCheck = nil
	ready := make(chan int, 1)
	var mainStarted atomic.Bool
	manager.OnStarted = func(string) { mainStarted.Store(true) }
	manager.OnOutput = func(_ string, line string) {
		if strings.HasPrefix(line, "CHILD-READY=") {
			pid, err := strconv.Atoi(strings.TrimPrefix(line, "CHILD-READY="))
			if err == nil {
				ready <- pid
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	exited := make(chan error, 1)
	go func() { exited <- orch.Run(ctx) }()
	defer func() { cancel(); _ = manager.Stop("api", time.Second); <-exited }()
	orch.Start([]string{"api"})
	pid := 0
	select {
	case pid = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("preparation tree did not become ready")
	}
	defer func() {
		if child, err := os.FindProcess(pid); err == nil {
			_ = child.Kill()
		}
	}()
	ownership, ok := manager.Ownership("api")
	if !ok || ownership.Stage != "pre_start" {
		t.Fatalf("not a real preparing owner: %+v, present=%t", ownership, ok)
	}
	observed := make(chan error, 1)
	orch.OnStartContainer = func(context.Context, string, int, *config.Container) error {
		if manager.IsRunning("api") || manager.IsAlive("api") {
			err := fmt.Errorf("old execution still owned at target launch")
			observed <- err
			return err
		}
		if platform.IsProcessAlive(pid) {
			err := fmt.Errorf("old preparation descendant still alive at target launch")
			observed <- err
			return err
		}
		if mainStarted.Load() {
			err := fmt.Errorf("old main process launched during mode replacement")
			observed <- err
			return err
		}
		observed <- nil
		return nil
	}
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
	if response.StatusCode != http.StatusOK || ack.OperationID == "" {
		t.Fatalf("acceptance=%d %+v", response.StatusCode, ack)
	}
	select {
	case err := <-observed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mode switch did not start replacement")
	}
	srv.waitForBackground()
	operation, ok := srv.lifecycle.read(ack.OperationID)
	if !ok || operation.State != "succeeded" {
		t.Fatalf("mode operation=%+v", operation)
	}
}
