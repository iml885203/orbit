package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/container"
	"github.com/moby/moby/client"
)

const runningRedisList = `[{"Id":"c1","State":"running","Labels":{"orbit.managed":"true","orbit.service":"redis"}}]`

// heldDockerList is a fake Docker API whose first container list blocks
// until released, so a test can finish a lifecycle step while a poll is in
// flight. Later lists answer with current.
type heldDockerList struct {
	listing chan struct{}
	release chan struct{}
}

func startHeldDockerList(t *testing.T, held, current string) (*heldDockerList, *client.Client) {
	t.Helper()
	h := &heldDockerList{listing: make(chan struct{}), release: make(chan struct{})}
	var listed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			if listed.CompareAndSwap(false, true) {
				close(h.listing)
				<-h.release
				_, _ = w.Write([]byte(held))
				return
			}
			_, _ = w.Write([]byte(current))
		// c1 exists only while the current list still shows it.
		case strings.HasSuffix(r.URL.Path, "/containers/c1/json") && current != "[]":
			_, _ = w.Write([]byte(`{"Id":"c1","State":{"Status":"running","Running":true,"StartedAt":"2026-11-04T00:00:00Z"}}`))
		default:
			http.Error(w, `{"message":"No such container: c1"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := client.New(
		client.WithHost("tcp://"+endpoint.Host),
		client.WithScheme("http"),
		client.WithHTTPClient(server.Client()),
		client.WithAPIVersion("1.52"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return h, cli
}

// runPollerAcross runs the daemon's poller wiring against cli and performs
// step while the first poll's container list is in flight. It returns once
// that poll's snapshot has been applied.
func runPollerAcross(t *testing.T, o *Orchestrator, h *heldDockerList, cli *client.Client, step func()) {
	t.Helper()
	cfg := twoContainerCfg()
	app := &App{Orchestrator: o, ContainerMgr: &container.Manager{}}
	poller := container.NewPoller(cli, "", 10*time.Millisecond)
	apply := app.newPoller(cfg, o).OnStateUpdate
	applied := make(chan struct{}, 1)
	poller.OnStateUpdate = func(listedAt time.Time, states map[string]container.ContainerState) {
		apply(listedAt, states)
		select {
		case applied <- struct{}{}:
		default:
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()
	stop := func() {
		cancel()
		<-done
	}
	t.Cleanup(stop)

	<-h.listing
	step()
	close(h.release)
	select {
	case <-applied:
	case <-time.After(5 * time.Second):
		t.Fatal("poll snapshot was never applied")
	}
	stop()
}

func TestPollListedBeforeContainerStartKeepsItHealthy(t *testing.T) {
	o := newTestOrchestrator(twoContainerCfg())
	h, cli := startHeldDockerList(t, `[]`, runningRedisList)

	runPollerAcross(t, o, h, cli, func() {
		o.mu.Lock()
		o.services["redis"].Transition(StateStarting)
		o.services["redis"].Transition(StateHealthy)
		o.mu.Unlock()
	})

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateHealthy {
		t.Fatalf("redis = %s (%q), want healthy: the empty list predates its start", info.State, info.StateReason)
	}
}

func TestPollListedBeforeContainerStopKeepsItStopped(t *testing.T) {
	o := newTestOrchestrator(twoContainerCfg())
	o.OnContainerSeen("redis", true)
	h, cli := startHeldDockerList(t, runningRedisList, `[]`)

	runPollerAcross(t, o, h, cli, func() {
		if err := o.StopService(context.Background(), "redis"); err != nil {
			t.Fatal(err)
		}
	})

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateStopped {
		t.Fatalf("redis = %s (%q), want stopped: the running list predates its stop", info.State, info.StateReason)
	}
}

func TestPassingProbesDoNotHideContainerRemoval(t *testing.T) {
	o := newTestOrchestrator(twoContainerCfg())
	o.OnContainerSeen("redis", true)
	o.mu.Lock()
	listedAt := o.services["redis"].stateChangedAt.Add(time.Nanosecond)
	o.services["redis"].Transition(StateHealthy) // a passing runtime probe
	o.mu.Unlock()

	o.OnContainerMissing("redis", listedAt)

	info, _ := o.GetServiceInfo("redis")
	if info.State != StateDegraded || info.StateReason != "container removed outside orbit" {
		t.Fatalf("redis = %s (%q), want removal reported", info.State, info.StateReason)
	}
}
