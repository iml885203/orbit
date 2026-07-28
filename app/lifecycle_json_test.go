package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

func TestBuildLogsJSONData(t *testing.T) {
	got := buildLogsJSONData("worker", 2, &daemon.LogsResponse{Lines: []string{"a", "b"}})
	if got.Service != "worker" {
		t.Fatalf("service = %q", got.Service)
	}
	if got.LinesRequested != 2 {
		t.Fatalf("lines_requested = %d", got.LinesRequested)
	}
	if !got.Truncated {
		t.Fatal("truncated = false, want true when returned lines equals requested lines")
	}
}

func TestBuildLogsJSONDataNilResponseUsesEmptyLines(t *testing.T) {
	got := buildLogsJSONData("svc", 10, nil)
	assertLogsJSONLinesEmpty(t, got)
}

func TestBuildLogsJSONDataNilResponseLinesUsesEmptyLines(t *testing.T) {
	got := buildLogsJSONData("svc", 10, &daemon.LogsResponse{})
	assertLogsJSONLinesEmpty(t, got)
}

func assertLogsJSONLinesEmpty(t *testing.T, got logsJSONData) {
	t.Helper()
	if got.Lines == nil {
		t.Fatal("Lines = nil, want non-nil empty slice")
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"lines":[]`)) {
		t.Fatalf("encoded lines = %s, want lines encoded as []", encoded)
	}
}

func TestWriteLogJSONEvent(t *testing.T) {
	var buf bytes.Buffer
	if err := writeLogJSONEvent(&buf, "worker", "ready"); err != nil {
		t.Fatalf("writeLogJSONEvent: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, buf.String())
	}
	if got["schema_version"] != "orbit.cli.v1" {
		t.Fatalf("schema_version = %v", got["schema_version"])
	}
	if got["type"] != "log" {
		t.Fatalf("type = %v", got["type"])
	}
	if got["service"] != "worker" || got["line"] != "ready" {
		t.Fatalf("event = %+v", got)
	}
}

func TestBuildLifecycleJSONDataClassifiesServices(t *testing.T) {
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "redis", Kind: daemon.ServiceKindContainer, State: "healthy"},
			{Name: "worker", Kind: daemon.ServiceKindService, State: "degraded"},
			{Name: "payments", Kind: daemon.ServiceKindService, State: "starting"},
		},
	}
	got := buildLifecycleJSONData(lifecycleJSONOptions{
		Operation:         "up",
		Message:           "starting 2 services",
		RequestedServices: []string{"worker", "payments"},
		FinalStatus:       status,
		TimedOutServices:  []string{"payments"},
	})
	if got.Operation != "up" {
		t.Fatalf("operation = %q", got.Operation)
	}
	if len(got.Services) != 2 {
		t.Fatalf("services count = %d, want 2", len(got.Services))
	}
	if len(got.DegradedServices) != 1 || got.DegradedServices[0] != "worker" {
		t.Fatalf("degraded_services = %+v", got.DegradedServices)
	}
	if len(got.TimedOutServices) != 1 || got.TimedOutServices[0] != "payments" {
		t.Fatalf("timed_out_services = %+v", got.TimedOutServices)
	}
}

func TestBuildLifecycleJSONDataEmptySlicesMarshalAsArrays(t *testing.T) {
	got := buildLifecycleJSONData(lifecycleJSONOptions{Operation: "up"})
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{
		`"requested_services":[]`,
		`"services":[]`,
		`"degraded_services":[]`,
		`"timed_out_services":[]`,
	} {
		if !bytes.Contains(encoded, []byte(field)) {
			t.Fatalf("encoded lifecycle payload = %s, want %s", encoded, field)
		}
	}
}

func TestLifecycleRecommendedActionsLeadThroughRecovery(t *testing.T) {
	got := lifecycleRecommendedActions([]string{"worker", "payments", "worker"})
	want := []string{
		"orbit status --json",
		"orbit logs worker --json",
		"orbit restart worker --json",
		"orbit logs payments --json",
		"orbit restart payments --json",
	}
	if len(got) != len(want) {
		t.Fatalf("actions count = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, action := range got {
		if action.Command != want[i] {
			t.Fatalf("actions[%d] = %q, want %q; actions = %+v", i, action.Command, want[i], got)
		}
	}
}

func TestLifecycleUpSuccessActionsHaveOnePrimaryNextStep(t *testing.T) {
	got := lifecycleUpSuccessActions()
	if len(got) != 1 || got[0].Command != "orbit open --json" {
		t.Fatalf("actions = %+v", got)
	}
}

func TestLifecycleServicesDone(t *testing.T) {
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "redis", State: "healthy"},
			{Name: "kafka", State: "healthy"},
		},
	}
	if !lifecycleServicesDone(status, []string{"redis", "kafka"}, "healthy") {
		t.Fatal("healthy services should be done")
	}
	if lifecycleServicesDone(status, []string{"redis", "missing"}, "healthy") {
		t.Fatal("missing service should not be done")
	}
}

func TestLifecycleServicesDoneStopped(t *testing.T) {
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "redis", State: "stopped"},
		},
	}
	if !lifecycleServicesDone(status, []string{"redis"}, "stopped") {
		t.Fatal("stopped service should be done")
	}
}

func TestLifecycleTerminalErrorFailsOnDegraded(t *testing.T) {
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "worker", State: "degraded"},
		},
	}
	err := lifecycleTerminalError(nil, status, []string{"worker"}, true)
	if err == nil || err.Error() != "worker failed to become healthy" {
		t.Fatalf("lifecycleTerminalError = %v, want worker failure", err)
	}
	if !errors.Is(err, cli.ErrServiceStartFailed) {
		t.Fatalf("error = %v, want service-start-failed classification", err)
	}
}

func TestLifecycleTerminalErrorIncludesObservedReason(t *testing.T) {
	status := &daemon.StatusResponse{Services: []daemon.ServiceStatus{
		{Name: "worker", State: "degraded", StateReason: "failed to start: address already in use"},
	}}
	err := lifecycleTerminalError(nil, status, []string{"worker"}, true)
	if err == nil || err.Error() != "worker failed to become healthy: failed to start: address already in use" {
		t.Fatalf("lifecycleTerminalError = %v", err)
	}
}

func TestLifecycleTerminalErrorFailsOnTerminalDependency(t *testing.T) {
	status := &daemon.StatusResponse{Services: []daemon.ServiceStatus{
		{
			Name:                "api",
			State:               "pending",
			PendingDependencies: []string{"redis"},
		},
		{
			Name:        "redis",
			State:       "degraded",
			StateReason: "container exited unexpectedly",
		},
	}}
	err := lifecycleTerminalError(nil, status, []string{"api"}, true)
	want := "api cannot start because dependency redis is unhealthy: container exited unexpectedly"
	if err == nil || err.Error() != want {
		t.Fatalf("lifecycleTerminalError = %v, want %q", err, want)
	}
	if !errors.Is(err, cli.ErrDependencyBlocked) {
		t.Fatalf("error = %v, want dependency-blocked classification", err)
	}
}

func TestLifecycleTerminalErrorFailsOnStoppedTransitiveDependency(t *testing.T) {
	status := &daemon.StatusResponse{Services: []daemon.ServiceStatus{
		{
			Name:                "api",
			State:               "pending",
			PendingDependencies: []string{"worker"},
		},
		{
			Name:                "worker",
			State:               "pending",
			PendingDependencies: []string{"redis"},
		},
		{Name: "redis", State: "stopped"},
	}}
	err := lifecycleTerminalError(nil, status, []string{"api"}, true)
	want := "api cannot start because dependency redis is unhealthy: stopped"
	if err == nil || err.Error() != want {
		t.Fatalf("lifecycleTerminalError = %v, want %q", err, want)
	}
}

func TestLifecycleRecommendedActionsIncludeFailedDependency(t *testing.T) {
	status := &daemon.StatusResponse{Services: []daemon.ServiceStatus{
		{
			Name:                "api",
			State:               "pending",
			PendingDependencies: []string{"redis"},
		},
		{Name: "redis", State: "degraded"},
	}}
	actions := lifecycleRecommendedActionsForStatus([]string{"api"}, status)
	var foundLogs, foundRestart bool
	for _, action := range actions {
		if action.Command == "orbit logs api --json" {
			t.Fatalf("actions = %+v, dependent has no logs before it starts", actions)
		}
		if action.Command == "orbit logs redis --json" {
			foundLogs = true
		}
		if action.Command == "orbit restart redis --json" {
			foundRestart = true
		}
	}
	if !foundLogs || !foundRestart {
		t.Fatalf("actions = %+v, want dependency logs and restart", actions)
	}
}

func TestLifecycleTerminalErrorFailsOnStopped(t *testing.T) {
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "worker", State: "stopped"},
		},
	}
	err := lifecycleTerminalError(nil, status, []string{"worker"}, true)
	if err == nil || err.Error() != "worker stopped before becoming healthy" {
		t.Fatalf("lifecycleTerminalError = %v, want stopped error", err)
	}
}

func TestLifecycleTerminalErrorAllowsStoppedWhenConfigured(t *testing.T) {
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "worker", State: "stopped"},
		},
	}
	if err := lifecycleTerminalError(nil, status, []string{"worker"}, false); err != nil {
		t.Fatalf("lifecycleTerminalError = %v, want nil", err)
	}
}

func TestLifecycleRestartHealthyWaitUsesStoppedAsTerminal(t *testing.T) {
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "worker", State: "stopped"},
		},
	}
	err := lifecycleTerminalError(nil, status, []string{"worker"}, true)
	if err == nil || err.Error() != "worker stopped before becoming healthy" {
		t.Fatalf("lifecycleTerminalError = %v, want stopped error", err)
	}
}

func TestLifecycleServiceExists(t *testing.T) {
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "worker", State: "healthy"},
		},
	}
	if !lifecycleServiceExists(status, "worker") {
		t.Fatal("worker should exist")
	}
	if lifecycleServiceExists(status, "missing") {
		t.Fatal("missing service should not exist")
	}
}

func TestLifecycleServicesDoneAcceptsPostStopStates(t *testing.T) {
	for _, state := range []string{"pending", "starting", "building", "healthy", "degraded"} {
		t.Run(state, func(t *testing.T) {
			status := &daemon.StatusResponse{
				Services: []daemon.ServiceStatus{
					{Name: "worker", State: state},
				},
			}
			if !lifecycleServicesDoneOrPast(status, []string{"worker"}, "stopped", lifecycleRestartPastStopState) {
				t.Fatalf("state %q should be accepted as past stopped", state)
			}
		})
	}
}

func TestLifecycleServicesDoneOrPastStillRejectsMissingService(t *testing.T) {
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "worker", State: "healthy"},
		},
	}
	if lifecycleServicesDoneOrPast(status, []string{"missing"}, "stopped", lifecycleRestartPastStopState) {
		t.Fatal("missing service should not be done")
	}
}

func TestDownNoDaemonErrorUsesJSONErrorPath(t *testing.T) {
	if err := downNoDaemonError(true); !errors.Is(err, daemon.ErrDaemonUnreachable) {
		t.Fatalf("downNoDaemonError(true) = %v, want daemon unreachable", err)
	}
	if err := downNoDaemonError(false); err != nil {
		t.Fatalf("downNoDaemonError(false) = %v, want nil", err)
	}
}

func TestLifecycleRestartObservedRejectsStaleHealthyWithUnchangedRestartCount(t *testing.T) {
	prior := 2
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "worker", State: "healthy", RestartCount: 2},
		},
	}
	if lifecycleRestartObserved(status, "worker", &prior) {
		t.Fatal("stale healthy service with unchanged restart count should not be considered restarted")
	}
}

func TestLifecycleRestartObservedAcceptsHealthyWithIncreasedRestartCount(t *testing.T) {
	prior := 2
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "worker", State: "healthy", RestartCount: 3},
		},
	}
	if !lifecycleRestartObserved(status, "worker", &prior) {
		t.Fatal("healthy service with increased restart count should be considered restarted")
	}
}

func TestLifecycleRestartObservedAcceptsIntermediateStateWithoutPriorCount(t *testing.T) {
	status := &daemon.StatusResponse{
		Services: []daemon.ServiceStatus{
			{Name: "worker", State: "starting"},
		},
	}
	if !lifecycleRestartObserved(status, "worker", nil) {
		t.Fatal("non-healthy intermediate state should be considered restart evidence without prior count")
	}
}
