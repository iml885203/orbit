package engine

import (
	"context"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

// startContainerForExit starts redis with a fake runtime ID and returns the
// lifecycle ctx its readiness probe runs under.
func startContainerForExit(t *testing.T, o *Orchestrator) context.Context {
	t.Helper()
	o.OnStartContainer = func(_ context.Context, _ string, _ *config.Container) (string, error) {
		return "runtime-1", nil
	}
	probeCtx := make(chan context.Context, 1)
	o.OnHealthCheck = func(ctx context.Context, _ string, _ int) error {
		probeCtx <- ctx
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = o.Run(ctx) }()
	o.Start([]string{"redis"})
	select {
	case c := <-probeCtx:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("readiness probe never started")
		return nil
	}
}

func TestContainerExitDuringStartupEndsTheWait(t *testing.T) {
	o := newTestOrchestrator(twoContainerCfg())
	probeCtx := startContainerForExit(t, o)

	o.OnContainerStopped("redis", StoppedContainer{
		ID:        "runtime-1",
		StartedAt: time.Now(),
		Exit:      ContainerExit{Code: 137, OOMKilled: true},
	})

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateDegraded || info.FailureKind != FailureKindRuntime || info.StateReason != containerExitedReason {
		t.Fatalf("state=%s kind=%q reason=%q, want degraded/runtime/%q", info.State, info.FailureKind, info.StateReason, containerExitedReason)
	}
	if info.ContainerExit == nil || *info.ContainerExit != (ContainerExit{Code: 137, OOMKilled: true}) {
		t.Fatalf("ContainerExit = %+v, want code 137 OOM", info.ContainerExit)
	}
	if probeCtx.Err() == nil {
		t.Fatal("readiness probing still running after the container exited")
	}
}

func TestContainerNotYetRunningDuringStartupIsNotAnExit(t *testing.T) {
	cases := map[string]StoppedContainer{
		"stale container awaiting removal": {ID: "stale", StartedAt: time.Now().Add(-time.Hour)},
		"created but not started":          {ID: "runtime-1"},
	}
	for name, stopped := range cases {
		t.Run(name, func(t *testing.T) {
			o := newTestOrchestrator(twoContainerCfg())
			probeCtx := startContainerForExit(t, o)

			o.OnContainerStopped("redis", stopped)

			if info, _ := o.GetServiceInfo("redis"); info.State != StateStarting {
				t.Fatalf("state = %s, want starting", info.State)
			}
			if probeCtx.Err() != nil {
				t.Fatal("readiness probing cancelled for a runtime that never ran")
			}
		})
	}
}

func TestContainerNotRunningBeforeItsStartReturnsIsNotAnExit(t *testing.T) {
	o := newTestOrchestrator(twoContainerCfg())
	release := make(chan struct{})
	o.OnStartContainer = func(_ context.Context, _ string, _ *config.Container) (string, error) {
		<-release
		return "runtime-1", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = o.Run(ctx) }()
	o.Start([]string{"redis"})
	waitForState(t, o, "redis", StateStarting)

	o.OnContainerStopped("redis", StoppedContainer{ID: "runtime-1", StartedAt: time.Now()})
	close(release)

	if info, _ := o.GetServiceInfo("redis"); info.State != StateStarting {
		t.Fatalf("state = %s, want starting", info.State)
	}
}

func TestContainerExitAfterHealthBudgetEndsRecovery(t *testing.T) {
	o := newTestOrchestrator(twoContainerCfg())
	probeCtx := startContainerForExit(t, o)
	gen := currentGeneration(t, o, "redis")
	_ = o.handleEvent(context.Background(), Event{Type: EventHealthFail, Service: "redis", Message: "health check for redis failed after 60 retries", FailureKind: FailureKindHealth, Generation: gen})

	o.OnContainerStopped("redis", StoppedContainer{ID: "runtime-1", StartedAt: time.Now(), Exit: ContainerExit{Code: 1}})

	info, _ := o.GetServiceInfo("redis")
	if info.FailureKind != FailureKindRuntime || info.ContainerExit == nil || info.ContainerExit.Code != 1 {
		t.Fatalf("kind=%q exit=%+v, want runtime with exit code 1", info.FailureKind, info.ContainerExit)
	}
	if probeCtx.Err() == nil {
		t.Fatal("recovery probing still running after the container exited")
	}
}

func TestLateProbeFailureKeepsTheContainerExit(t *testing.T) {
	o := newTestOrchestrator(twoContainerCfg())
	startContainerForExit(t, o)
	gen := currentGeneration(t, o, "redis")
	o.OnContainerStopped("redis", StoppedContainer{ID: "runtime-1", StartedAt: time.Now(), Exit: ContainerExit{Code: 2}})

	_ = o.handleEvent(context.Background(), Event{Type: EventHealthFail, Service: "redis", Message: "health check for redis failed after 60 retries", FailureKind: FailureKindHealth, Generation: gen})

	info, _ := o.GetServiceInfo("redis")
	if info.FailureKind != FailureKindRuntime || info.StateReason != containerExitedReason {
		t.Fatalf("kind=%q reason=%q, want the exit to stand", info.FailureKind, info.StateReason)
	}
}

func TestProbeSuccessQueuedBeforeTheExitDoesNotHeal(t *testing.T) {
	o := newTestOrchestrator(twoContainerCfg())
	startContainerForExit(t, o)
	gen := currentGeneration(t, o, "redis")
	o.OnContainerStopped("redis", StoppedContainer{ID: "runtime-1", StartedAt: time.Now()})

	_ = o.handleEvent(context.Background(), Event{Type: EventHealthOK, Service: "redis", Generation: gen})

	if info, _ := o.GetServiceInfo("redis"); info.State != StateDegraded {
		t.Fatalf("state = %s, want degraded", info.State)
	}
}

func TestExternalStartAfterStartupExitHeals(t *testing.T) {
	o := newTestOrchestrator(twoContainerCfg())
	startContainerForExit(t, o)
	exitedRun := time.Now().Add(-time.Minute)
	o.OnContainerStopped("redis", StoppedContainer{ID: "runtime-1", StartedAt: exitedRun, Exit: ContainerExit{Code: 1}})

	o.OnContainerObserved("redis", true, time.Now())

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateHealthy || info.ContainerExit != nil {
		t.Fatalf("state=%s exit=%+v, want healthy with the exit cleared", info.State, info.ContainerExit)
	}
}

func TestHealthyContainerExitRecordsExitDetails(t *testing.T) {
	o := newTestOrchestrator(twoContainerCfg())
	o.OnContainerSeen("redis", true)
	o.OnContainerObserved("redis", false, time.Time{})

	o.OnContainerStopped("redis", StoppedContainer{ID: "adopted", StartedAt: time.Now(), Exit: ContainerExit{Code: 139}})

	info, _ := o.GetServiceInfo("redis")
	if info.ContainerExit == nil || info.ContainerExit.Code != 139 {
		t.Fatalf("ContainerExit = %+v, want code 139", info.ContainerExit)
	}
}

func TestUninspectedExitLeavesExitDetailsUnknown(t *testing.T) {
	o := newTestOrchestrator(twoContainerCfg())
	o.OnContainerSeen("redis", true)
	o.OnContainerObserved("redis", false, time.Time{})

	o.OnContainerStopped("redis", StoppedContainer{ID: "adopted"})
	o.OnContainerStopped("redis", StoppedContainer{ID: "adopted", StartedAt: time.Now(), Exit: ContainerExit{Code: 3}})

	info, _ := o.GetServiceInfo("redis")
	if info.ContainerExit == nil || info.ContainerExit.Code != 3 {
		t.Fatalf("ContainerExit = %+v, want the first inspected exit (code 3)", info.ContainerExit)
	}
}

func TestRestartClearsContainerExit(t *testing.T) {
	info := &ServiceInfo{State: StateDegraded, ContainerExit: &ContainerExit{Code: 1}}
	info.Transition(StateStarting)
	if info.ContainerExit != nil {
		t.Fatal("ContainerExit survived a restart")
	}
}

func waitForState(t *testing.T, o *Orchestrator, name string, want ServiceState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, _ := o.GetServiceInfo(name); info.State == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	info, _ := o.GetServiceInfo(name)
	t.Fatalf("%s state = %s, want %s", name, info.State, want)
}

func currentGeneration(t *testing.T, o *Orchestrator, name string) int {
	t.Helper()
	info, _ := o.GetServiceInfo(name)
	return info.Generation
}
