package engine

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/health"
	"github.com/iml885203/orbit/process"
)

func TestHealthProtocolFailuresReachStateReason(t *testing.T) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	cfg := &config.Config{Services: map[string]*config.Service{
		"api": {
			Name: "api",
			HealthCheck: &config.HealthCheckConfig{
				Type: "http", Port: port, Path: "/health",
				Timeout: 100 * time.Millisecond, Interval: time.Millisecond, Retries: 1,
			},
		},
	}}
	holder := config.NewHolder(cfg)
	orch := NewOrchestrator(holder, nil, nil)
	orch.services["api"].State = StateStarting
	orch.services["api"].Generation = 1
	checker := health.NewChecker(nil, nil)
	app := &App{Orchestrator: orch, ProcessMgr: process.NewManager()}
	app.wireHealthCallbacks(checker, holder, orch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := orch.OnHealthCheck(ctx, "api", 1); err != nil {
		t.Fatal(err)
	}
	var event Event
	select {
	case event = <-orch.events:
	case <-time.After(time.Second):
		t.Fatal("health failure event was not emitted")
	}
	if err := orch.handleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	info, _ := orch.GetServiceInfo("api")
	if info.State != StateDegraded || !strings.Contains(info.StateReason, "h2c:") || !strings.Contains(info.StateReason, "HTTP/1.1:") {
		t.Fatalf("state = %s reason = %q", info.State, info.StateReason)
	}
}
