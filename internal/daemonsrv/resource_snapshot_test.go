package daemonsrv

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/engine"
)

func TestResourcesAggregatesRuntimeDependenciesAndFeatureContributors(t *testing.T) {
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer probe.Close()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(probe.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Containers: map[string]*config.Container{"redis": {Name: "redis", Image: "redis:7.4", Ports: map[string]config.PortDef{"redis": {Host: 36379, Target: 6379}}}},
		Services:   map[string]*config.Service{"api": {Name: "api", DependsOn: []string{"redis"}, URL: "http://localhost:3000"}},
	}
	srv := newTestServer(t, cfg)
	o := srv.app.Orchestrator
	o.OnStartProcess = func(context.Context, string, int, *config.Config, *config.Service) error { return nil }
	o.OnStopProcess = func(string) error { return nil }
	o.OnHealthCheck = func(context.Context, string, int) error { return nil }
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); _ = o.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("event loop leaked")
		}
	})
	o.OnContainerSeen("redis", true)
	o.Start([]string{"api"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, _ := o.GetServiceInfo("api")
		if info.State == engine.StateStarting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("api never started")
		}
		time.Sleep(time.Millisecond)
	}
	for range 2 {
		if err := o.RestartService(ctx, "api"); err != nil {
			t.Fatal(err)
		}
		for {
			info, _ := o.GetServiceInfo("api")
			if info.State == engine.StateStarting {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("api never restarted")
			}
			time.Sleep(time.Millisecond)
		}
	}
	if err := srv.app.HealthChecker.WaitForHealthy(ctx, "api", &config.HealthCheckConfig{Type: "http", Port: port, Path: "/health", Retries: 1, Interval: time.Millisecond}, nil); err == nil {
		t.Fatal("failed health probe accepted")
	}
	srv.app.HealthChecker.MarkRecovering("api", 0)
	info, _ := o.GetServiceInfo("api")
	o.Events() <- engine.Event{Type: engine.EventHealthFail, Service: "api", Generation: info.Generation, Message: "health check failed", FailureKind: engine.FailureKindHealth}
	for {
		info, _ = o.GetServiceInfo("api")
		if info.State == engine.StateDegraded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("health failure never reached status")
		}
		time.Sleep(time.Millisecond)
	}
	srv.AddResourceContributor(func(context.Context) []ResourceSnapshot {
		return []ResourceSnapshot{{Name: "CatalogDB", Type: "database", Parent: "redis", State: "baseline"}}
	})
	response := httptest.NewRecorder()
	srv.handleResources(response, httptest.NewRequest(http.MethodGet, "/api/resources", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("resources: %d %s", response.Code, response.Body.String())
	}
	var result ResourcesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != ResourceSchemaVersion || len(result.Resources) != 3 {
		t.Fatalf("resources: %+v", result)
	}
	resources := map[string]ResourceSnapshot{}
	for _, resource := range result.Resources {
		resources[resource.Name] = resource
	}
	redis, api := resources["redis"], resources["api"]
	if redis.Type != "container" || redis.State != "healthy" || redis.Properties["image"] != "redis:7.4" || redis.Properties["port:redis"] != "36379" {
		t.Fatalf("container runtime: %+v", redis)
	}
	if api.Type != "service" || api.StateReason != "health check failed" || api.FailureKind != "health" || strings.Join(api.DependsOn, ",") != "redis" || strings.Join(api.URLs, ",") != "http://localhost:3000" {
		t.Fatalf("service runtime: %+v", api)
	}
	if api.Properties["restarts"] != "2" {
		t.Fatalf("restart count lost: %+v", api.Properties)
	}
	if api.Health == nil || !api.Health.Recovering || api.Health.Attempts != 1 || api.Health.MaxRetries != 1 {
		t.Fatalf("health progress: %+v", api.Health)
	}
	if result.Resources[2].Name != "CatalogDB" || result.Resources[2].Parent != "redis" {
		t.Fatalf("feature resource ordering: %+v", result.Resources)
	}
	o.Events() <- engine.Event{Type: engine.EventProcessExited, Service: "api", Generation: info.Generation, Message: "exited: exit status 1", Evidence: "application failed"}
	for {
		info, _ = o.GetServiceInfo("api")
		if info.FailureEvidence != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("process evidence never reached status")
		}
		time.Sleep(time.Millisecond)
	}
	response = httptest.NewRecorder()
	srv.handleResources(response, httptest.NewRequest(http.MethodGet, "/api/resources", nil))
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, resource := range result.Resources {
		if resource.Name == "api" {
			found = true
			if resource.StateReason != "exited: exit status 1 — application failed" {
				t.Fatalf("lost failure evidence: %+v", resource)
			}
		}
	}
	if !found {
		t.Fatal("api disappeared from resources after process exit")
	}

}

func TestSortResources_ParentsFirstDeterministic(t *testing.T) {
	in := []ResourceSnapshot{
		{Name: "/callback/pay", Type: "route", Parent: "tunnel-5001"},
		{Name: "redis", Type: "container"},
		{Name: "CatalogDB", Type: "database", Parent: "sql-server"},
		{Name: "api", Type: "service"},
		{Name: "tunnel-5001", Type: "tunnel"},
	}
	sortResources(in)
	if in[len(in)-1].Parent == "" || in[0].Parent != "" {
		t.Errorf("expected parents before children, got %+v", in)
	}
	for i := 1; i < len(in); i++ {
		a, b := in[i-1], in[i]
		if (a.Parent == "") == (b.Parent == "") && (a.Type > b.Type || (a.Type == b.Type && a.Name > b.Name)) {
			t.Errorf("unstable order at %d: %+v before %+v", i, a, b)
		}
	}
}
