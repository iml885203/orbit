package daemon

import (
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestSnapshotWorkloads_MapsStatusAndDependencies(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis"},
		},
		Services: map[string]*config.Service{
			"api": {Name: "api", DependsOn: []string{"redis"}},
		},
	}
	statuses := []ResourceStatus{
		{Name: "redis", Kind: "container", State: "healthy", Image: "redis:7.4", Ports: map[string]int{"redis": 36379}},
		{Name: "api", Kind: "service", State: "degraded", StateReason: "health check failed",
			URL: "http://localhost:3000", RestartCount: 2,
			HealthProgress: &HealthProgressInfo{Attempts: 12, MaxRetries: 12, Recovering: true}},
	}

	out := snapshotWorkloads(dependencyMap(cfg), statuses)
	if len(out) != 2 {
		t.Fatalf("got %d resources, want 2", len(out))
	}

	redis := out[0]
	if redis.Type != "container" || redis.State != "healthy" || redis.Properties["image"] != "redis:7.4" {
		t.Errorf("redis snapshot wrong: %+v", redis)
	}
	if redis.Properties["port:redis"] != "36379" {
		t.Errorf("expected port property, got %v", redis.Properties)
	}

	api := out[1]
	if api.Type != "service" || api.StateReason != "health check failed" {
		t.Errorf("api snapshot wrong: %+v", api)
	}
	if len(api.DependsOn) != 1 || api.DependsOn[0] != "redis" {
		t.Errorf("expected depends_on [redis], got %v", api.DependsOn)
	}
	if len(api.URLs) != 1 || api.URLs[0] != "http://localhost:3000" {
		t.Errorf("expected url, got %v", api.URLs)
	}
	if api.Properties["restarts"] != "2" {
		t.Errorf("expected restarts property, got %v", api.Properties)
	}
	if api.Health == nil || !api.Health.Recovering {
		t.Errorf("expected health progress with recovering, got %+v", api.Health)
	}
}

func TestResourceFailureSummaryKeepsLifecycleCauseAndEvidence(t *testing.T) {
	status := ResourceStatus{
		StateReason:     "exited: exit status 1",
		FailureEvidence: "ModuleNotFoundError: No module named 'humanize'",
	}
	want := "exited: exit status 1 — ModuleNotFoundError: No module named 'humanize'"
	if got := resourceFailureSummary(status); got != want {
		t.Fatalf("summary = %q, want %q", got, want)
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
