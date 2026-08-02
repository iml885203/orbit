package engine

import (
	"reflect"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestPlanConfigReconcileAddingResourcePreservesRunningResources(t *testing.T) {
	oldCfg := reconcileTestConfig()
	newCfg := reconcileTestConfig()
	newCfg.Services["worker"] = &config.Service{Name: "worker", Command: "worker"}

	plan := PlanConfigReconcile(oldCfg, newCfg, map[string]bool{"redis": true, "api": true}, nil)
	if plan.RestartRequired || len(plan.Stop) != 0 || len(plan.Restart) != 0 {
		t.Fatalf("plan = %+v, want no runtime interruption", plan)
	}
}

func TestPlanConfigReconcileRestartsChangedResourceAndDependents(t *testing.T) {
	oldCfg := reconcileTestConfig()
	newCfg := reconcileTestConfig()
	newCfg.Containers["redis"].Image = "redis:8"

	plan := PlanConfigReconcile(oldCfg, newCfg, map[string]bool{"redis": true, "api": true}, nil)
	if !reflect.DeepEqual(plan.Stop, []string{"api", "redis"}) ||
		!reflect.DeepEqual(plan.Restart, []string{"api", "redis"}) {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPlanConfigReconcileRequiresDaemonRestartForPollInterval(t *testing.T) {
	oldCfg := reconcileTestConfig()
	newCfg := reconcileTestConfig()
	newCfg.Settings.DockerPollInterval++

	plan := PlanConfigReconcile(oldCfg, newCfg, map[string]bool{"redis": true}, nil)
	if !plan.RestartRequired {
		t.Fatalf("plan = %+v, want daemon restart", plan)
	}
}

func TestApplyConfigPreservesUnchangedRuntimeState(t *testing.T) {
	oldCfg := reconcileTestConfig()
	holder := config.NewHolder(oldCfg)
	orchestrator := NewOrchestrator(holder, nil, nil)
	orchestrator.MarkServiceHealthy("redis")
	orchestrator.MarkServiceHealthy("api")
	orchestrator.mu.Lock()
	orchestrator.services["api"].Generation = 7
	orchestrator.mu.Unlock()

	newCfg := reconcileTestConfig()
	newCfg.Services["worker"] = &config.Service{Name: "worker", Command: "worker"}
	orchestrator.ApplyConfig(newCfg, nil, nil)

	api, exists := orchestrator.GetServiceInfo("api")
	if !exists || api.State != StateHealthy || api.Generation != 7 {
		t.Fatalf("api runtime = %+v, exists=%v", api, exists)
	}
	worker, exists := orchestrator.GetServiceInfo("worker")
	if !exists || worker.State != StateStopped {
		t.Fatalf("worker runtime = %+v, exists=%v", worker, exists)
	}
}

func reconcileTestConfig() *config.Config {
	return &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis", Image: "redis:7"},
		},
		Services: map[string]*config.Service{
			"api": {Name: "api", Command: "api", DependsOn: []string{"redis"}},
		},
	}
}
