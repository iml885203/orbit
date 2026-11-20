package engine

import (
	"context"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
)

func pendingDependencyConfig(dependency string) *config.Config {
	return &config.Config{Services: map[string]*config.Service{
		"api":      {Name: "api", DependsOn: []string{dependency}},
		dependency: {Name: dependency},
	}}
}

func nextReadyEvent(t *testing.T, o *Orchestrator) Event {
	t.Helper()
	select {
	case evt := <-o.events:
		if evt.Type != EventDepsReady || evt.Service != "api" {
			t.Fatalf("event = %+v, want api dependency readiness", evt)
		}
		return evt
	case <-time.After(time.Second):
		t.Fatal("api dependency readiness was not scheduled")
		return Event{}
	}
}

func TestDependencyReadyEventCannotOutlivePendingIntent(t *testing.T) {
	for _, next := range []string{"stopped", "removed", "replaced", "ready replacement", "removed and readded"} {
		t.Run(next, func(t *testing.T) {
			o := testOrchestrator(pendingDependencyConfig("redis"))
			o.Start([]string{"api"})
			o.MarkServiceHealthy("redis")
			oldReady := nextReadyEvent(t, o)
			if err := o.StopService(context.Background(), "api"); err != nil {
				t.Fatal(err)
			}
			want := StateStopped
			switch next {
			case "removed":
				o.ApplyConfig(&config.Config{}, nil, nil)
			case "replaced":
				o.ApplyConfig(pendingDependencyConfig("database"), nil, nil)
				o.Start([]string{"api"})
				want = StatePending
			case "removed and readded":
				o.ApplyConfig(&config.Config{}, nil, nil)
				o.ApplyConfig(pendingDependencyConfig("redis"), nil, nil)
				o.Start([]string{"api"})
				want = StatePending
			case "ready replacement":
				o.Start([]string{"api"})
				want = StatePending
			}
			if err := o.handleEvent(context.Background(), oldReady); err != nil {
				t.Fatal(err)
			}
			info, exists := o.GetServiceInfo("api")
			if next == "removed" {
				if exists {
					t.Fatal("obsolete dependency event recreated removed service")
				}
				return
			}
			if !exists || info.State != want {
				t.Fatalf("old dependency event changed current intent: info=%+v exists=%v", info, exists)
			}
			if next == "ready replacement" {
				current := nextReadyEvent(t, o)
				if err := o.handleEvent(context.Background(), current); err != nil {
					t.Fatal(err)
				}
				if info, _ := o.GetServiceInfo("api"); info.State != StateStarting {
					t.Fatalf("current intent state = %s, want starting", info.State)
				}
			}
		})
	}
}

func TestDependencyReadinessDoesNotConsumeStoppedWaiter(t *testing.T) {
	o := testOrchestrator(pendingDependencyConfig("redis"))
	o.Start([]string{"api"})
	if err := o.StopService(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	o.MarkServiceHealthy("redis")
	info, _ := o.GetServiceInfo("api")
	if info.State != StateStopped || !info.PendingDeps["redis"] {
		t.Fatalf("dependency readiness consumed a stopped lifecycle's waiter: %+v", info)
	}
}

func TestQueuedStartRechecksCurrentDependencyAvailability(t *testing.T) {
	o := testOrchestrator(pendingDependencyConfig("redis"))
	o.MarkServiceHealthy("redis")
	o.Start([]string{"api"})
	ready := nextReadyEvent(t, o)
	if err := o.StopService(context.Background(), "redis"); err != nil {
		t.Fatal(err)
	}
	if err := o.handleEvent(context.Background(), ready); err != nil {
		t.Fatal(err)
	}
	info, _ := o.GetServiceInfo("api")
	if info.State != StatePending || !info.PendingDeps["redis"] {
		t.Fatalf("queued start ignored dependency availability: %+v", info)
	}
	o.MarkServiceHealthy("redis")
	if err := o.handleEvent(context.Background(), nextReadyEvent(t, o)); err != nil {
		t.Fatal(err)
	}
	if info, _ := o.GetServiceInfo("api"); info.State != StateStarting {
		t.Fatalf("current dependency readiness did not start service: %+v", info)
	}
}
