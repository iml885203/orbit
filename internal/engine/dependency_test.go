package engine

import (
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestBuildDAG_TopologicalOrder(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis":      {Name: "redis"},
			"sql-server": {Name: "sql-server"},
		},
		Services: map[string]*config.Service{
			"api": {
				Name:      "api",
				DependsOn: []string{"redis", "sql-server"},
			},
			"frontend": {
				Name:      "frontend",
				DependsOn: []string{"api"},
			},
		},
	}

	order, deps := BuildDAG(cfg)

	if len(order) != 4 {
		t.Fatalf("order has %d items, want 4", len(order))
	}

	// Build position map
	pos := make(map[string]int)
	for i, name := range order {
		pos[name] = i
	}

	// Verify: redis and sql-server before api, api before frontend
	if pos["redis"] > pos["api"] {
		t.Errorf("redis (pos %d) should come before api (pos %d)", pos["redis"], pos["api"])
	}
	if pos["sql-server"] > pos["api"] {
		t.Errorf("sql-server (pos %d) should come before api (pos %d)", pos["sql-server"], pos["api"])
	}
	if pos["api"] > pos["frontend"] {
		t.Errorf("api (pos %d) should come before frontend (pos %d)", pos["api"], pos["frontend"])
	}

	// Verify deps map
	if len(deps["api"]) != 2 {
		t.Errorf("api deps = %d, want 2", len(deps["api"]))
	}
	if len(deps["frontend"]) != 1 {
		t.Errorf("frontend deps = %d, want 1", len(deps["frontend"]))
	}
	if len(deps["redis"]) != 0 {
		t.Errorf("redis deps = %d, want 0", len(deps["redis"]))
	}
}

func TestBuildDAG_FiltersDetachedEdges(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"frontend": {Name: "frontend", Kind: "frontend", DependsOn: []string{"api"}},
			"api": {Name: "api", Kind: "backend", DependsOn: []string{"redis"}},
		},
		Containers: map[string]*config.Container{
			"redis": {Name: "redis", Kind: "infra"},
		},
	}

	// detached: frontend → api
	detached := map[string][]string{
		"frontend": {"api"},
	}

	order, deps := BuildDAGWithDetached(cfg, detached)

	if len(deps["frontend"]) != 0 {
		t.Errorf("frontend deps = %v, want empty (detached)", deps["frontend"])
	}
	if len(deps["api"]) != 1 || deps["api"][0] != "redis" {
		t.Errorf("api deps = %v, want [redis]", deps["api"])
	}
	if len(order) != 3 {
		t.Errorf("order length = %d, want 3", len(order))
	}
}

func TestFilterEnabledServices(t *testing.T) {
	cfg := &config.Config{
		Groups: map[string]config.Group{
			"back_office": {Enabled: true, Services: []string{"payments", "worker"}},
			"player_site": {Enabled: false, Services: []string{"frontend", "api"}},
		},
		Containers: map[string]*config.Container{
			"redis":      {Name: "redis"},
			"sql-server": {Name: "sql-server"},
		},
		Services: map[string]*config.Service{
			"payments": {Name: "payments", DependsOn: []string{"worker"}},
			"worker":   {Name: "worker", DependsOn: []string{"redis", "sql-server"}},
			"frontend":      {Name: "frontend", DependsOn: []string{"api"}},
			"api":      {Name: "api", DependsOn: []string{"redis"}},
		},
	}

	enabled := FilterEnabledServices(cfg, nil)

	// Back office enabled: payments, worker + all containers
	if !enabled["payments"] {
		t.Error("payments should be enabled")
	}
	if !enabled["worker"] {
		t.Error("worker should be enabled")
	}
	if !enabled["redis"] {
		t.Error("redis should be enabled (container)")
	}
	if !enabled["sql-server"] {
		t.Error("sql-server should be enabled (container)")
	}
	// Player site disabled
	if enabled["frontend"] {
		t.Error("frontend should NOT be enabled")
	}
	if enabled["api"] {
		t.Error("api should NOT be enabled")
	}
}

func TestFilterEnabledServices_CLIOverride(t *testing.T) {
	cfg := &config.Config{
		Groups: map[string]config.Group{
			"back_office": {Enabled: false, Services: []string{"payments"}},
			"player_site": {Enabled: false, Services: []string{"frontend"}},
		},
		Containers: map[string]*config.Container{
			"redis": {Name: "redis"},
		},
		Services: map[string]*config.Service{
			"payments": {Name: "payments"},
			"frontend":      {Name: "frontend"},
		},
	}

	// CLI override enables player_site even though config has it disabled
	enabled := FilterEnabledServices(cfg, []string{"player_site"})

	if !enabled["frontend"] {
		t.Error("frontend should be enabled via CLI override")
	}
	if enabled["payments"] {
		t.Error("payments should NOT be enabled (not in CLI override)")
	}
	if !enabled["redis"] {
		t.Error("redis should always be enabled (container)")
	}
}
