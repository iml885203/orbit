package container

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

func observationTestClient(t *testing.T, handler http.Handler) *client.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := client.New(client.WithHost("tcp://"+endpoint.Host), client.WithScheme("http"), client.WithHTTPClient(server.Client()), client.WithAPIVersion("1.52"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

func observationList(w http.ResponseWriter, names ...string) {
	rows := make([]map[string]any, 0, len(names))
	for _, name := range names {
		rows = append(rows, map[string]any{"Id": name, "State": "running", "Labels": map[string]string{labelManaged: "true", labelNamespace: "observation-test", labelService: name}})
	}
	_ = json.NewEncoder(w).Encode(rows)
}

func TestFailedInspectKeepsPreviousObservationAndRecovers(t *testing.T) {
	var failing atomic.Bool
	failing.Store(true)
	cli := observationTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			observationList(w, "api", "sidecar")
		case strings.HasSuffix(r.URL.Path, "/sidecar/json") && failing.Load():
			http.Error(w, `{"message":"controlled inspect failure"}`, http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte(`{"State":{"Status":"exited","Running":false,"StartedAt":"2026-11-19T00:00:00Z"}}`))
		}
	}))
	poller := NewPoller(cli, "observation-test", time.Second)
	poller.lastStates = map[string]ContainerState{"api": {Name: "api", Running: true}, "sidecar": {Name: "sidecar", Running: true}}
	updates, drift, unavailable := 0, 0, 0
	poller.OnStateUpdate = func(time.Time, map[string]ContainerState) { updates++ }
	poller.OnDrift = func(string, ContainerState) { drift++ }
	poller.OnUnavailable = func(error) { unavailable++ }
	poller.poll(context.Background())
	poller.poll(context.Background())
	if updates != 0 || drift != 0 || unavailable != 1 || !poller.lastStates["api"].Running {
		t.Fatalf("partial failure published fresh state: updates=%d drift=%d unavailable=%d previous=%+v", updates, drift, unavailable, poller.lastStates)
	}
	failing.Store(false)
	poller.poll(context.Background())
	if updates != 1 || drift != 2 || poller.unavailable || poller.consecutiveFailures != 0 {
		t.Fatalf("did not recover: updates=%d drift=%d unavailable=%v failures=%d", updates, drift, poller.unavailable, poller.consecutiveFailures)
	}
}

func TestDockerObservationStallRetriesReportsUnavailableAndRecovers(t *testing.T) {
	for _, stage := range []string{"list", "inspect"} {
		t.Run(stage, func(t *testing.T) {
			var stalled atomic.Bool
			stalled.Store(true)
			var requests atomic.Int64
			unavailable := make(chan error, 1)
			recovered := make(chan struct{}, 1)
			cli := observationTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				isList := strings.HasSuffix(r.URL.Path, "/containers/json")
				if (stage == "list" && isList) || (stage == "inspect" && !isList) {
					requests.Add(1)
					if stalled.Load() {
						<-r.Context().Done()
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if isList {
					observationList(w, "api")
					return
				}
				_, _ = w.Write([]byte(`{"State":{"Status":"running","Running":true,"StartedAt":"2026-11-19T00:00:00Z"}}`))
			}))
			poller := NewPoller(cli, "observation-test", 5*time.Millisecond)
			poller.requestTimeout = 20 * time.Millisecond
			poller.cycleTimeout = 100 * time.Millisecond
			poller.OnUnavailable = func(err error) {
				select {
				case unavailable <- err:
				default:
				}
			}
			poller.OnStateUpdate = func(time.Time, map[string]ContainerState) {
				select {
				case recovered <- struct{}{}:
				default:
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { poller.Run(ctx); close(done) }()
			select {
			case err := <-unavailable:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("missing deadline evidence: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("stalled Docker observation never reported unavailable")
			}
			select {
			case <-recovered:
				t.Fatal("stalled cycle published a fresh observation")
			default:
			}
			if count := requests.Load(); count < 2 {
				t.Fatalf("retry count=%d; expected later retries", count)
			}
			stalled.Store(false)
			select {
			case <-recovered:
			case <-time.After(time.Second):
				t.Fatal("poller failed to recover after Docker became responsive")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("poller did not stop after cancellation")
			}
		})
	}
}

func TestDockerObservationCycleBudgetBoundsManyIndividuallyFastInspects(t *testing.T) {
	var inspections atomic.Int64
	cli := observationTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			observationList(w, "one", "two", "three", "four", "five")
			return
		}
		inspections.Add(1)
		select {
		case <-time.After(20 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"State":{"Status":"running","Running":true}}`))
	}))
	poller := NewPoller(cli, "observation-test", time.Second)
	poller.requestTimeout = time.Second
	poller.cycleTimeout = 45 * time.Millisecond
	updates := 0
	poller.OnStateUpdate = func(time.Time, map[string]ContainerState) { updates++ }
	start := time.Now()
	poller.poll(context.Background())
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond || inspections.Load() >= 5 || updates != 0 || poller.consecutiveFailures != 1 {
		t.Fatalf("cycle not bounded: elapsed=%s inspections=%d updates=%d failures=%d", elapsed, inspections.Load(), updates, poller.consecutiveFailures)
	}
}

func TestDockerObservationLifetimeCancellationInterruptsStalledRequest(t *testing.T) {
	started := make(chan struct{})
	cli := observationTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	poller := NewPoller(cli, "observation-test", time.Millisecond)
	unavailable := 0
	poller.OnUnavailable = func(error) { unavailable++ }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { poller.Run(ctx); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stalled request ignored lifetime cancellation")
	}
	if unavailable != 0 || poller.consecutiveFailures != 0 {
		t.Fatal("normal shutdown reported Docker unavailable")
	}
}
