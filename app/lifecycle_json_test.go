package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

func TestBuildLogsJSONData(t *testing.T) {
	got := buildLogsJSONData("worker", 2, &daemon.LogsResponse{Lines: []string{"a", "b"}})
	if got.Resource != "worker" {
		t.Fatalf("resource = %q", got.Resource)
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
	if got["resource"] != "worker" || got["line"] != "ready" {
		t.Fatalf("event = %+v", got)
	}
	if _, ok := got["service"]; ok {
		t.Fatalf("event contains legacy service field: %+v", got)
	}
}

func TestBuildLifecycleJSONDataClassifiesResources(t *testing.T) {
	status := &daemon.StatusResponse{
		Resources: []daemon.ResourceStatus{
			{Name: "redis", Kind: daemon.ResourceKindContainer, State: "healthy"},
			{Name: "worker", Kind: daemon.ResourceKindService, State: "degraded"},
			{Name: "payments", Kind: daemon.ResourceKindService, State: "starting"},
		},
	}
	got := buildLifecycleJSONData(lifecycleJSONOptions{
		Operation:          "up",
		Message:            "starting 2 resources",
		RequestedResources: []string{"worker", "payments"},
		FinalStatus:        status,
		TimedOutResources:  []string{"payments"},
	})
	if got.Operation != "up" {
		t.Fatalf("operation = %q", got.Operation)
	}
	if len(got.Resources) != 2 {
		t.Fatalf("resources count = %d, want 2", len(got.Resources))
	}
	if len(got.DegradedResources) != 1 || got.DegradedResources[0] != "worker" {
		t.Fatalf("degraded_resources = %+v", got.DegradedResources)
	}
	if len(got.TimedOutResources) != 1 || got.TimedOutResources[0] != "payments" {
		t.Fatalf("timed_out_resources = %+v", got.TimedOutResources)
	}
}

func TestBuildLifecycleJSONDataEmptySlicesMarshalAsArrays(t *testing.T) {
	got := buildLifecycleJSONData(lifecycleJSONOptions{Operation: "up"})
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{
		`"requested_resources":[]`,
		`"resources":[]`,
		`"degraded_resources":[]`,
		`"timed_out_resources":[]`,
	} {
		if !bytes.Contains(encoded, []byte(field)) {
			t.Fatalf("encoded lifecycle payload = %s, want %s", encoded, field)
		}
	}
	for _, legacy := range []string{
		`"requested_services"`,
		`"services"`,
		`"degraded_services"`,
		`"timed_out_services"`,
	} {
		if bytes.Contains(encoded, []byte(legacy)) {
			t.Fatalf("encoded lifecycle payload = %s, contains legacy field %s", encoded, legacy)
		}
	}
}

func TestDownCompletionExplainsOrbitRemainsReady(t *testing.T) {
	got := buildLifecycleJSONData(lifecycleJSONOptions{
		Operation: "down",
		Message:   downCompletionMessage,
	})
	if got.Message != "Environment stopped. Orbit is ready for the next 'orbit up'." {
		t.Fatalf("message = %q", got.Message)
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
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "aaa-unrelated", Kind: daemon.ResourceKindService, State: "healthy", URL: "http://localhost:2999"},
		{Name: "redis-ui", Kind: daemon.ResourceKindContainer, State: "healthy", URL: "http://localhost:8081"},
		{Name: "web", Kind: daemon.ResourceKindService, State: "healthy", URL: "http://localhost:3000"},
		{Name: "admin", Kind: daemon.ResourceKindService, State: "healthy", URL: "http://localhost:3001"},
	}}
	got := lifecycleUpSuccessActions([]string{"redis-ui", "web", "admin"}, status)
	if len(got) != 1 || got[0].Command != "orbit open admin --json" {
		t.Fatalf("actions = %+v", got)
	}
	if !strings.Contains(got[0].Reason, "http://localhost:3001") {
		t.Fatalf("reason = %q, want concrete URL", got[0].Reason)
	}
}

func TestLifecycleUpSuccessActionsFallBackToDashboard(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "redis", Kind: daemon.ResourceKindContainer, State: "healthy"},
		{Name: "web", Kind: daemon.ResourceKindService, State: "stopped", URL: "http://localhost:3000"},
	}}
	got := lifecycleUpSuccessActions([]string{"redis", "web"}, status)
	if len(got) != 1 || got[0].Command != "orbit open --json" {
		t.Fatalf("actions = %+v", got)
	}
}

func TestNoLogsRecoveryActionsRevalidateResolvedPortConflict(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	portNumber := listener.Addr().(*net.TCPAddr).Port
	resource := &daemon.ResourceStatus{
		Name:  "api",
		State: "degraded",
		PortConflict: &daemon.ResourcePortConflict{
			Port:     portNumber,
			Resource: "api",
		},
	}

	occupied := noLogsRecoveryActions(resource)
	if len(occupied) != 1 || strings.HasPrefix(occupied[0].Command, "orbit up") {
		t.Fatalf("occupied actions = %+v", occupied)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	released := noLogsRecoveryActions(resource)
	if len(released) != 1 || released[0].Command != "orbit up api --json" {
		t.Fatalf("released actions = %+v", released)
	}
}

func TestLogsRecoveryActionsLeadFromCrashOutputToTargetedRestart(t *testing.T) {
	resource := &daemon.ResourceStatus{
		Name:          "api",
		State:         "degraded",
		StateReason:   "exited: signal: killed",
		LogsAvailable: true,
	}
	actions := logsRecoveryActions(resource)
	if len(actions) != 1 || actions[0].Command != "orbit restart api --json" {
		t.Fatalf("actions = %+v", actions)
	}
	if !strings.Contains(actions[0].Reason, "after reviewing its exit output") {
		t.Fatalf("reason = %q", actions[0].Reason)
	}
}

func TestLogsRecoveryActionsKeepRecoveringServiceOnStatus(t *testing.T) {
	resource := &daemon.ResourceStatus{
		Name:          "api",
		State:         "degraded",
		LogsAvailable: true,
		HealthProgress: &daemon.HealthProgressInfo{
			Recovering: true,
		},
	}
	actions := logsRecoveryActions(resource)
	if len(actions) != 1 || actions[0].Command != "orbit status --json" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestLifecycleServicesDone(t *testing.T) {
	status := &daemon.StatusResponse{
		Resources: []daemon.ResourceStatus{
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
		Resources: []daemon.ResourceStatus{
			{Name: "redis", State: "stopped"},
		},
	}
	if !lifecycleServicesDone(status, []string{"redis"}, "stopped") {
		t.Fatal("stopped service should be done")
	}
}

func TestLifecycleTerminalErrorFailsOnDegraded(t *testing.T) {
	status := &daemon.StatusResponse{
		Resources: []daemon.ResourceStatus{
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
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "worker", State: "degraded", StateReason: "failed to start: address already in use"},
	}}
	err := lifecycleTerminalError(nil, status, []string{"worker"}, true)
	if err == nil || err.Error() != "worker failed to become healthy: failed to start: address already in use" {
		t.Fatalf("lifecycleTerminalError = %v", err)
	}
}

func TestLifecycleTerminalErrorPreservesPortConflict(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{{
		Name:        "redis",
		State:       "degraded",
		StateReason: "cannot start redis: port 26379 is already in use",
		PortConflict: &daemon.ResourcePortConflict{
			Port:           26379,
			Resource:       "redis",
			PID:            "42",
			InspectCommand: "ps -p 42 -o pid,comm,args=",
		},
	}}}
	err := lifecycleTerminalError(nil, status, []string{"redis"}, true)
	var conflict *cli.ResourcePortConflictError
	if !errors.As(err, &conflict) || conflict.Port != 26379 || conflict.Resource != "redis" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestLifecycleBlockedDependencyPreservesPortConflict(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{
			Name:                "api",
			State:               "pending",
			PendingDependencies: []string{"redis"},
		},
		{
			Name:  "redis",
			State: "degraded",
			PortConflict: &daemon.ResourcePortConflict{
				Port:           26379,
				Resource:       "redis",
				InspectCommand: "lsof -nP -iTCP:26379 -sTCP:LISTEN",
			},
		},
	}}
	err := lifecycleTerminalError(nil, status, []string{"api"}, true)
	var conflict *cli.ResourcePortConflictError
	if !errors.As(err, &conflict) || conflict.Resource != "redis" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestLifecycleTerminalErrorFailsOnTerminalDependency(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
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
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
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
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
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

func TestLifecycleRecommendedActionsExcludeHealthyResources(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "mongo", State: "degraded", StateReason: "container exited unexpectedly"},
		{Name: "redis", State: "healthy"},
	}}
	actions := lifecycleRecommendedActionsForStatus([]string{"mongo", "redis"}, status)
	got := make([]string, 0, len(actions))
	for _, action := range actions {
		got = append(got, action.Command)
	}
	want := []string{
		"orbit status --json",
		"orbit logs mongo --json",
		"orbit restart mongo --json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
}

func TestLifecycleRecommendedActionsFollowPendingChainToRootResource(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{
			Name:                "web",
			State:               "pending",
			PendingDependencies: []string{"api"},
		},
		{
			Name:                "api",
			State:               "pending",
			PendingDependencies: []string{"database"},
		},
		{Name: "database", State: "starting"},
	}}
	actions := lifecycleRecommendedActionsForStatus([]string{"api", "database", "web"}, status)
	got := make([]string, 0, len(actions))
	for _, action := range actions {
		got = append(got, action.Command)
	}
	want := []string{
		"orbit status --json",
		"orbit logs database --json",
		"orbit restart database --json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
}

func TestLifecycleRecommendedActionsPrioritizeTerminalFailureOverConvergingResource(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{
			Name:                "api",
			State:               "pending",
			PendingDependencies: []string{"cache", "database"},
		},
		{Name: "cache", State: "starting"},
		{Name: "database", State: "degraded", StateReason: "container exited unexpectedly"},
	}}
	actions := lifecycleRecommendedActionsForStatus([]string{"api", "cache", "database"}, status)
	got := make([]string, 0, len(actions))
	for _, action := range actions {
		got = append(got, action.Command)
	}
	want := []string{
		"orbit status --json",
		"orbit logs database --json",
		"orbit restart database --json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
}

func TestLifecycleTerminalErrorFailsOnStopped(t *testing.T) {
	status := &daemon.StatusResponse{
		Resources: []daemon.ResourceStatus{
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
		Resources: []daemon.ResourceStatus{
			{Name: "worker", State: "stopped"},
		},
	}
	if err := lifecycleTerminalError(nil, status, []string{"worker"}, false); err != nil {
		t.Fatalf("lifecycleTerminalError = %v, want nil", err)
	}
}

func TestLifecycleRestartHealthyWaitUsesStoppedAsTerminal(t *testing.T) {
	status := &daemon.StatusResponse{
		Resources: []daemon.ResourceStatus{
			{Name: "worker", State: "stopped"},
		},
	}
	err := lifecycleTerminalError(nil, status, []string{"worker"}, true)
	if err == nil || err.Error() != "worker stopped before becoming healthy" {
		t.Fatalf("lifecycleTerminalError = %v, want stopped error", err)
	}
}

func TestLifecycleResourceExists(t *testing.T) {
	status := &daemon.StatusResponse{
		Resources: []daemon.ResourceStatus{
			{Name: "worker", State: "healthy"},
		},
	}
	if !lifecycleResourceExists(status, "worker") {
		t.Fatal("worker should exist")
	}
	if lifecycleResourceExists(status, "missing") {
		t.Fatal("missing service should not exist")
	}
}

func TestLifecycleServicesDoneAcceptsPostStopStates(t *testing.T) {
	for _, state := range []string{"pending", "starting", "building", "healthy", "degraded"} {
		t.Run(state, func(t *testing.T) {
			status := &daemon.StatusResponse{
				Resources: []daemon.ResourceStatus{
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
		Resources: []daemon.ResourceStatus{
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
		Resources: []daemon.ResourceStatus{
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
		Resources: []daemon.ResourceStatus{
			{Name: "worker", State: "healthy", RestartCount: 3},
		},
	}
	if !lifecycleRestartObserved(status, "worker", &prior) {
		t.Fatal("healthy service with increased restart count should be considered restarted")
	}
}

func TestLifecycleRestartObservedAcceptsIntermediateStateWithoutPriorCount(t *testing.T) {
	status := &daemon.StatusResponse{
		Resources: []daemon.ResourceStatus{
			{Name: "worker", State: "starting"},
		},
	}
	if !lifecycleRestartObserved(status, "worker", nil) {
		t.Fatal("non-healthy intermediate state should be considered restart evidence without prior count")
	}
}
