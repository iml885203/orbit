package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/iml885203/orbit/internal/config"
)

func TestModeTransitionCancellationAfterRetirementDoesNotLaunchTarget(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api"}}, Containers: map[string]*config.Container{"api": {Name: "api", Image: "api"}}}
	orch := testOrchestrator(cfg)
	orch.MarkServiceHealthy("api")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	orch.OnStopProcess = func(string) error { cancel(); return nil }
	if err := orch.ChangeServiceMode(ctx, "api", "container"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	info, _ := orch.GetServiceInfo("api")
	if info.Kind != "service" || info.State != StateStopped || info.RestartCount != 0 {
		t.Fatalf("canceled transition committed target: %+v", info)
	}
	select {
	case event := <-orch.events:
		t.Fatalf("canceled transition queued startup: %+v", event)
	default:
	}
}
