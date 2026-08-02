package app

import (
	"reflect"
	"testing"

	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
)

func TestRunningEnvironmentResourcesPreservesActiveIntent(t *testing.T) {
	resources := []daemon.ResourceStatus{
		{Name: "stopped", State: "stopped"},
		{Name: "stopping", State: "stopping"},
		{Name: "healthy", State: "healthy"},
		{Name: "degraded", State: "degraded"},
		{Name: "starting", State: "starting"},
		{Name: "pending", State: "pending"},
		{Name: "building", State: "building"},
		{Name: "restarting", State: "restarting"},
	}

	got := runningEnvironmentResources(resources)
	want := []string{"building", "degraded", "healthy", "pending", "restarting", "starting"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runningEnvironmentResources() = %v, want %v", got, want)
	}
}

func TestRestorableEnvironmentResourcesSeparatesRemovedConfig(t *testing.T) {
	previouslyRunning := []string{"api", "database", "removed"}
	available := []daemon.ResourceStatus{
		{Name: "database", State: "stopped"},
		{Name: "api", State: "stopped"},
		{Name: "new", State: "stopped"},
	}

	restored, unavailable := restorableEnvironmentResources(previouslyRunning, available)
	if !reflect.DeepEqual(restored, []string{"api", "database"}) {
		t.Fatalf("restored = %v", restored)
	}
	if !reflect.DeepEqual(unavailable, []string{"removed"}) {
		t.Fatalf("unavailable = %v", unavailable)
	}
}

func TestEnvironmentApplySeparatesRestoredIntentFromNewDependencies(t *testing.T) {
	restored := []string{"api", "web"}
	affected := []string{"redis", "web", "api"}

	startedDependencies := daemonsrv.AdditionalResourceNames(restored, affected)
	if !reflect.DeepEqual(startedDependencies, []string{"redis"}) {
		t.Fatalf("started dependencies = %v", startedDependencies)
	}

	data := buildEnvironmentApplyJSONData(environmentApplyResult{
		Applied:             true,
		DaemonRunning:       true,
		PreviouslyRunning:   restored,
		RestoredResources:   restored,
		StartedDependencies: startedDependencies,
	})
	if !reflect.DeepEqual(data.RestoredResources, restored) ||
		!reflect.DeepEqual(data.StartedDependencies, []string{"redis"}) {
		t.Fatalf("apply data = %+v", data)
	}
}
