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

func TestLifecycleUpSuccessActionsReturnToOnlyHealthyFrontendAfterBackendRecovery(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "shop", Kind: daemon.ResourceKindService, Role: "frontend", State: "healthy", URL: "http://localhost:3000"},
		{Name: "inventory-api", Kind: daemon.ResourceKindService, Role: "backend", State: "healthy", URL: "http://localhost:8080"},
		{Name: "admin", Kind: daemon.ResourceKindService, Role: "backend", State: "healthy", URL: "http://localhost:3001"},
	}}
	got := lifecycleUpSuccessActions([]string{"inventory-api"}, status)
	if len(got) != 1 || got[0].Command != "orbit open shop --json" {
		t.Fatalf("actions = %+v", got)
	}
}

func TestLifecycleUpSuccessActionsDoNotGuessAmongUnrelatedFrontends(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "shop", Kind: daemon.ResourceKindService, Role: "frontend", State: "healthy", URL: "http://localhost:3000"},
		{Name: "admin", Kind: daemon.ResourceKindService, Role: "frontend", State: "healthy", URL: "http://localhost:3001"},
		{Name: "inventory-api", Kind: daemon.ResourceKindService, Role: "backend", State: "healthy", URL: "http://localhost:8080"},
	}}
	got := lifecycleUpSuccessActions([]string{"inventory-api"}, status)
	if len(got) != 1 || got[0].Command != "orbit open inventory-api --json" {
		t.Fatalf("actions = %+v", got)
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
	actions := logsRecoveryActions(resource, "")
	if len(actions) != 1 || actions[0].Command != "orbit restart api --json" {
		t.Fatalf("actions = %+v", actions)
	}
	if !strings.Contains(actions[0].Reason, "after reviewing its exit output") {
		t.Fatalf("reason = %q", actions[0].Reason)
	}
}

func TestLogsRecoveryActionsSetUpDependenciesBeforeTargetedRestart(t *testing.T) {
	resource := &daemon.ResourceStatus{
		Name:          "api",
		State:         "degraded",
		StateReason:   "exited: exit status 1",
		LogsAvailable: true,
	}
	setup := "python3 -m pip install -r /workspace/api/requirements.txt"
	actions := logsRecoveryActions(resource, setup)
	want := setup + " && orbit restart api --json"
	if len(actions) != 1 || actions[0].Command != want {
		t.Fatalf("actions = %+v, want %q", actions, want)
	}
	if !strings.Contains(actions[0].Reason, "declared project dependencies") {
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
	actions := logsRecoveryActions(resource, "")
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
		{Name: "redis", State: "degraded", LogsAvailable: true},
	}}
	actions := lifecycleRecommendedActionsForStatus([]string{"api"}, status)
	if len(actions) != 1 || actions[0].Command != "orbit logs redis --json" {
		t.Fatalf("actions = %+v, want only dependency logs", actions)
	}
}

func TestLifecycleRecommendedActionsExcludeHealthyResources(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "mongo", State: "degraded", StateReason: "container exited unexpectedly", LogsAvailable: true},
		{Name: "redis", State: "healthy"},
	}}
	actions := lifecycleRecommendedActionsForStatus([]string{"mongo", "redis"}, status)
	got := make([]string, 0, len(actions))
	for _, action := range actions {
		got = append(got, action.Command)
	}
	want := []string{"orbit logs mongo --json"}
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
		{Name: "database", State: "degraded", StateReason: "container exited unexpectedly", LogsAvailable: true},
	}}
	actions := lifecycleRecommendedActionsForStatus([]string{"api", "cache", "database"}, status)
	got := make([]string, 0, len(actions))
	for _, action := range actions {
		got = append(got, action.Command)
	}
	want := []string{"orbit logs database --json"}
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
