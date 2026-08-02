package app

import (
	"reflect"
	"testing"

	"github.com/iml885203/orbit/daemon"
)

func TestBuildUpFailureJSONDataCollectsEvidencePerUnhealthyResource(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "db", State: "healthy"},
		{Name: "api", State: "degraded", StateReason: "exited with code 1"},
		{Name: "worker", State: "pending"},
		{Name: "unwatched", State: "degraded", StateReason: "not requested"},
	}}
	tails := map[string][]string{
		"api":    {"panic: connection refused", "exit status 1"},
		"worker": nil,
	}

	data := buildUpFailureJSONData([]string{"db", "api", "worker"}, status, func(name string) []string {
		return tails[name]
	})

	if data.Operation != "up" {
		t.Fatalf("operation = %q", data.Operation)
	}
	want := []upFailedResource{
		{Name: "api", State: "degraded", StateReason: "exited with code 1", LogTail: tails["api"]},
		{Name: "worker", State: "pending"},
	}
	if !reflect.DeepEqual(data.FailedResources, want) {
		t.Fatalf("failed_resources = %+v, want %+v", data.FailedResources, want)
	}
}

func TestBuildUpFailureJSONDataFallsBackToHealthProbeError(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{
			Name:           "api",
			State:          "degraded",
			HealthProgress: &daemon.HealthProgressInfo{LastErr: "dial tcp 127.0.0.1:8080: connection refused"},
		},
	}}

	data := buildUpFailureJSONData([]string{"api"}, status, func(string) []string { return nil })

	if len(data.FailedResources) != 1 {
		t.Fatalf("failed_resources = %+v, want one entry", data.FailedResources)
	}
	if got := data.FailedResources[0].StateReason; got != "dial tcp 127.0.0.1:8080: connection refused" {
		t.Fatalf("state_reason = %q, want the health probe error", got)
	}
}

func TestBuildUpFailureJSONDataSurvivesMissingStatus(t *testing.T) {
	data := buildUpFailureJSONData([]string{"api"}, nil, func(string) []string { return nil })
	if len(data.FailedResources) != 0 {
		t.Fatalf("failed_resources = %+v, want none without a status snapshot", data.FailedResources)
	}
	if !reflect.DeepEqual(data.RequestedResources, []string{"api"}) {
		t.Fatalf("requested_resources = %+v", data.RequestedResources)
	}
}

func TestRunningResourceNamesIgnoresStoppedAndPending(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "db", State: "healthy"},
		{Name: "api", State: "starting"},
		{Name: "worker", State: "stopped"},
		{Name: "queued", State: "pending"},
		{Name: "flaky", State: "degraded"},
	}}
	got := runningResourceNames(status)
	want := []string{"db", "api", "flaky"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("running resources = %v, want %v", got, want)
	}
}

func TestRelocatedPortsReportsOnlyMovedAutoPorts(t *testing.T) {
	cfg, _ := loadEnvInfoFixture(t)
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "api", State: "healthy", Ports: map[string]int{"http": 18081}},
		{Name: "db", State: "healthy", Ports: map[string]int{"pg": 15432}},
	}}

	moves := relocatedPorts(cfg, status)

	if len(moves) != 1 {
		t.Fatalf("moves = %+v, want only the moved auto port", moves)
	}
	if moves[0].Resource != "api" || moves[0].Label != "http" || moves[0].Preferred != 18080 || moves[0].Actual != 18081 {
		t.Fatalf("move = %+v", moves[0])
	}

	stable := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "api", State: "healthy", Ports: map[string]int{"http": 18080}},
	}}
	if got := relocatedPorts(cfg, stable); len(got) != 0 {
		t.Fatalf("moves = %+v, want none when the preference held", got)
	}
}
