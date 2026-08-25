package app

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/daemon"
)

func TestLifecycleProgressPhaseChangesStayTerseAndDeduplicated(t *testing.T) {
	started := time.Now()
	var output bytes.Buffer
	progress := dormantLifecycleProgress(context.Background(), &output, started, 30*time.Second)

	progress.phaseAt(phaseEnsuringDaemon, started)
	progress.phaseAt(phaseEnsuringDaemon, started.Add(time.Second))
	progress.phaseAt(phaseCheckingEnvironment, started.Add(2*time.Second))

	if got, want := output.String(), "⋯ ensuring daemon\n⋯ checking environment\n"; got != want {
		t.Fatalf("progress = %q, want %q", got, want)
	}
	if strings.Contains(output.String(), "elapsed") || strings.Contains(output.String(), "remaining") {
		t.Fatalf("phase changes included timing: %q", output.String())
	}
}

func TestLifecycleProgressHeartbeatReportsOperationTiming(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithDeadline(context.Background(), started.Add(5*time.Minute))
	defer cancel()
	var output bytes.Buffer
	progress := dormantLifecycleProgress(ctx, &output, started, 30*time.Second)
	progress.phaseAt(phaseApplyingEnvironment, started)

	progress.heartbeat(started.Add(29 * time.Second))
	progress.heartbeat(started.Add(30 * time.Second))

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q, want phase plus one heartbeat", output.String())
	}
	if got, want := lines[1], "⋯ applying environment (elapsed 30s, about 4m30s remaining)"; got != want {
		t.Fatalf("heartbeat = %q, want %q", got, want)
	}
}

func TestLifecycleProgressResourceHeartbeatSuppressesGenericReadiness(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithDeadline(context.Background(), started.Add(5*time.Minute))
	defer cancel()
	var output bytes.Buffer
	progress := dormantLifecycleProgress(ctx, &output, started, 30*time.Second)
	progress.phaseAt(phaseWaitingForReadiness, started)
	firstStatus := started.Add(progressCheckInterval)
	progress.ObserveResources([]daemon.ResourceStatus{{Name: "api", State: "starting"}}, firstStatus)

	progress.heartbeat(started.Add(30 * time.Second))
	progress.heartbeat(firstStatus.Add(30 * time.Second))
	progress.heartbeat(firstStatus.Add(31 * time.Second))

	got := output.String()
	if !strings.Contains(got, "⋯ api still starting (elapsed 30s, about 4m29s remaining)\n") {
		t.Fatalf("resource heartbeat missing: %q", got)
	}
	if strings.Count(got, "waiting for readiness") != 1 {
		t.Fatalf("generic readiness was not suppressed: %q", got)
	}
}

func TestLifecycleProgressUsesOneInjectedCadenceForResourceAndGenericHeartbeats(t *testing.T) {
	started := time.Now()
	var output bytes.Buffer
	progress := dormantLifecycleProgress(context.Background(), &output, started, 2*time.Second)
	progress.phaseAt(phaseWaitingForReadiness, started)
	progress.ObserveResources([]daemon.ResourceStatus{{Name: "api", State: "building"}}, started)

	progress.heartbeat(started.Add(2 * time.Second))
	progress.heartbeat(started.Add(3 * time.Second))
	progress.heartbeat(started.Add(4 * time.Second))

	got := output.String()
	if strings.Count(got, "api still building") != 2 {
		t.Fatalf("resource heartbeat cadence = %q", got)
	}
	if strings.Count(got, "waiting for readiness") != 1 {
		t.Fatalf("generic heartbeat duplicated resource cadence: %q", got)
	}
}

func TestLifecycleProgressGenericReadinessCoversNonHeartbeatableState(t *testing.T) {
	started := time.Now()
	var output bytes.Buffer
	progress := dormantLifecycleProgress(context.Background(), &output, started, 30*time.Second)
	progress.phaseAt(phaseWaitingForReadiness, started)
	progress.ObserveResources([]daemon.ResourceStatus{{Name: "api", State: "pending"}}, started)

	progress.heartbeat(started.Add(30 * time.Second))

	if got := output.String(); !strings.Contains(got, "⋯ waiting for readiness (elapsed 30s)\n") {
		t.Fatalf("generic heartbeat missing: %q", got)
	}
}

func TestLifecycleProgressRemainingBudgetClampsAtZero(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithDeadline(context.Background(), started.Add(time.Minute))
	defer cancel()
	progress := dormantLifecycleProgress(ctx, &bytes.Buffer{}, started, 30*time.Second)

	if got, want := progress.timing(started.Add(2*time.Minute), 2*time.Minute), " (elapsed 2m0s, about 0s remaining)"; got != want {
		t.Fatalf("timing = %q, want %q", got, want)
	}
}

func TestLifecycleProgressCloseStopsAndJoinsReporter(t *testing.T) {
	started := time.Now()
	ticks := make(chan time.Time, 1)
	var stopped atomic.Bool
	var output bytes.Buffer
	progress := newLifecycleProgressWithTicker(
		context.Background(),
		&output,
		started,
		30*time.Second,
		ticks,
		func() { stopped.Store(true) },
	)
	progress.phaseAt(phaseEnsuringDaemon, started)
	progress.Close()
	ticks <- started.Add(30 * time.Second)

	if !stopped.Load() {
		t.Fatal("ticker was not stopped")
	}
	if got, want := output.String(), "⋯ ensuring daemon\n"; got != want {
		t.Fatalf("late heartbeat after close: %q", got)
	}
}

func dormantLifecycleProgress(ctx context.Context, output *bytes.Buffer, started time.Time, interval time.Duration) *lifecycleProgress {
	return &lifecycleProgress{
		ctx:       ctx,
		out:       output,
		started:   started,
		interval:  interval,
		resources: map[string]progressSnapshot{},
	}
}
