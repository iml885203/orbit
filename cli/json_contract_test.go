package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

func decodeEnvelope(t *testing.T, raw []byte) JSONEnvelope {
	t.Helper()
	var got JSONEnvelope
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, raw)
	}
	return got
}

func TestWriteJSONSuccess(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONSuccess(&buf, "orbit doctor --json", map[string]string{"ran_at": "now"},
		[]JSONAction{{Command: "orbit status --json", Reason: "Inspect current service state.", Destructive: false}},
	)
	if err != nil {
		t.Fatalf("WriteJSONSuccess: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.SchemaVersion != "orbit.cli.v1" {
		t.Fatalf("schema_version = %q", got.SchemaVersion)
	}
	if !got.OK {
		t.Fatal("ok = false, want true")
	}
	if got.Command != "orbit doctor --json" {
		t.Fatalf("command = %q", got.Command)
	}
	if got.Error != nil {
		t.Fatalf("error present on success: %+v", got.Error)
	}
	if len(got.RecommendedActions) != 1 || got.RecommendedActions[0].Command != "orbit status --json" {
		t.Fatalf("recommended_actions wrong: %+v", got.RecommendedActions)
	}
}

func TestWriteJSONErrorClassifiesDaemonUnreachable(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit restart worker --json", fmt.Errorf("dial daemon: %w", daemon.ErrDaemonUnreachable))
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.OK {
		t.Fatal("ok = true, want false")
	}
	if got.Error == nil {
		t.Fatal("error missing")
	}
	if got.Error.Code != "daemon_unreachable" {
		t.Fatalf("code = %q", got.Error.Code)
	}
	if !got.Error.Retryable {
		t.Fatal("retryable = false, want true")
	}
	if got.Error.Message != "Orbit is not running." {
		t.Fatalf("message = %q", got.Error.Message)
	}
	if got.Error.Hint != "Run 'orbit up' to start the selected environment." {
		t.Fatalf("hint = %q", got.Error.Hint)
	}
	if got.Error.NextCommand != "orbit up --json" {
		t.Fatalf("next_command = %q", got.Error.NextCommand)
	}
	if len(got.RecommendedActions) != 1 ||
		got.RecommendedActions[0].Command != "orbit up --json" {
		t.Fatalf("actions = %+v, want only orbit up", got.RecommendedActions)
	}
}

func TestWriteJSONErrorGivesSchemaMismatchOneAdvancingAction(t *testing.T) {
	orbitHome := t.TempDir()
	t.Setenv("ORBIT_HOME", orbitHome)
	tests := []struct {
		name        string
		version     string
		path        string
		code        string
		nextCommand string
	}{
		{
			name:        "shared environment is older",
			version:     "2",
			path:        filepath.Join(orbitHome, "envs", "team.yaml"),
			code:        "environment_schema_outdated",
			nextCommand: "orbit env sync --json",
		},
		{
			name:        "Orbit is older",
			version:     "4",
			path:        filepath.Join(t.TempDir(), "orbit.yaml"),
			code:        "environment_schema_newer",
			nextCommand: "orbit update --json",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := config.CheckVersion(tc.version, tc.path)
			if writeErr := WriteJSONError(&buf, "orbit up --json", err); writeErr != nil {
				t.Fatalf("WriteJSONError: %v", writeErr)
			}
			got := decodeEnvelope(t, buf.Bytes())
			if got.Error == nil || got.Error.Code != tc.code {
				t.Fatalf("error = %+v, want code %q", got.Error, tc.code)
			}
			if got.Error.NextCommand != tc.nextCommand {
				t.Fatalf("next command = %q, want %q", got.Error.NextCommand, tc.nextCommand)
			}
			if len(got.RecommendedActions) != 1 ||
				got.RecommendedActions[0].Command != tc.nextCommand {
				t.Fatalf("actions = %+v, want only %q", got.RecommendedActions, tc.nextCommand)
			}
		})
	}
}

func TestWriteJSONErrorDoesNotSyncProjectLocalSchema(t *testing.T) {
	var buf bytes.Buffer
	err := config.CheckVersion("2", filepath.Join(t.TempDir(), "orbit.yaml"))
	if writeErr := WriteJSONError(&buf, "orbit up --json", err); writeErr != nil {
		t.Fatal(writeErr)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "environment_schema_outdated" {
		t.Fatalf("error = %+v", got.Error)
	}
	if got.Error.NextCommand != "" || len(got.RecommendedActions) != 0 {
		t.Fatalf("project-local migration recommended sync: %+v", got)
	}
}

func TestWriteJSONErrorAlignsNextCommandWithReplacementAction(t *testing.T) {
	var buf bytes.Buffer
	inspectCommand := "lsof -nP -iTCP:28080 -sTCP:LISTEN"
	err := WithJSONReplacementActions(
		NewChecksFailedError("port 28080 is already in use"),
		[]JSONAction{{
			Command:     inspectCommand,
			Reason:      "Inspect the process that owns the port.",
			Destructive: false,
		}},
	)
	if writeErr := WriteJSONError(&buf, "orbit doctor --json", err); writeErr != nil {
		t.Fatalf("WriteJSONError: %v", writeErr)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.NextCommand != inspectCommand {
		t.Fatalf("error = %+v", got.Error)
	}
	if len(got.RecommendedActions) != 1 || got.RecommendedActions[0].Command != got.Error.NextCommand {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestWriteJSONErrorClassifiesConfigMismatch(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit sqlserver list --json", &daemon.ConfigMismatchError{
		Requested: "/tmp/selected.yaml",
		Running:   "/tmp/running.yaml",
	})
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "env_mismatch" {
		t.Fatalf("error = %+v", got.Error)
	}
	if !got.Error.Retryable {
		t.Fatal("retryable = false, want true")
	}
	if got.Error.NextCommand != `orbit daemon restart -c "/tmp/selected.yaml"` {
		t.Fatalf("next_command = %q", got.Error.NextCommand)
	}
	if len(got.RecommendedActions) != 1 || got.RecommendedActions[0].Command != got.Error.NextCommand {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestWriteJSONErrorClassifiesUnknownOlderDaemonConfig(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit sqlserver list --json", &daemon.ConfigMismatchError{
		Requested: "/tmp/selected.yaml",
	})
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "env_mismatch" {
		t.Fatalf("error = %+v", got.Error)
	}
	if !got.Error.Retryable ||
		got.Error.NextCommand != `orbit daemon restart -c "/tmp/selected.yaml"` ||
		!strings.Contains(got.Error.Hint, "verify which environment") {
		t.Fatalf("error recovery = %+v", got.Error)
	}
}

func TestWriteJSONErrorClassifiesPendingEnvironmentChanges(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit up api --json", &daemon.ConfigStaleError{Reason: "env file edited"})
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "environment_changed" {
		t.Fatalf("error = %+v", got.Error)
	}
	if !got.Error.Retryable || got.Error.NextCommand != "orbit env apply --json" {
		t.Fatalf("error recovery = %+v", got.Error)
	}
}

func TestWriteJSONErrorClassifiesPendingOrbitUpdate(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit up --json", &daemon.UpdateRequiredError{
		Running:   "v0.0.1",
		Installed: "v0.0.2",
	})
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "orbit_update_pending" {
		t.Fatalf("error = %+v", got.Error)
	}
	if !got.Error.Retryable || got.Error.NextCommand != "orbit daemon restart --json" {
		t.Fatalf("error recovery = %+v", got.Error)
	}
	if len(got.RecommendedActions) != 1 ||
		got.RecommendedActions[0].Command != "orbit daemon restart --json" {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestWriteJSONErrorPreservesUpdateRestartCommand(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit up --json", &daemon.UpdateRequiredError{
		RestartCommand:     `"/active/orbit" daemon restart`,
		RestartJSONCommand: `"/active/orbit" daemon restart --json`,
	})
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.NextCommand != `"/active/orbit" daemon restart --json` {
		t.Fatalf("error = %+v", got.Error)
	}
	if len(got.RecommendedActions) != 1 ||
		got.RecommendedActions[0].Command != `"/active/orbit" daemon restart --json` {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestWriteJSONErrorClassifiesInvalidEnvironmentWithExactRetry(t *testing.T) {
	var buf bytes.Buffer
	retry := `orbit switch "/tmp/broken env.yaml" --json`
	err := WithJSONActions(
		NewInvalidEnvironmentError("validate target environment: invalid YAML"),
		[]JSONAction{{
			Command:     retry,
			Reason:      "Retry the switch after fixing the reported environment file.",
			Destructive: false,
		}},
	)
	if writeErr := WriteJSONError(&buf, retry, err); writeErr != nil {
		t.Fatalf("WriteJSONError: %v", writeErr)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "invalid_environment" {
		t.Fatalf("error = %+v", got.Error)
	}
	if !got.Error.Retryable || got.Error.NextCommand != "" {
		t.Fatalf("error recovery = %+v", got.Error)
	}
	if len(got.RecommendedActions) != 1 || got.RecommendedActions[0].Command != retry {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestWriteJSONErrorClassifiesInvalidArgumentWithoutRecoveryDetour(t *testing.T) {
	var buf bytes.Buffer
	err := NewInvalidArgumentError("service names and --infra cannot be used together")
	if writeErr := WriteJSONError(&buf, "orbit up api --infra --json", err); writeErr != nil {
		t.Fatalf("WriteJSONError: %v", writeErr)
	}
	var envelope JSONEnvelope
	if decodeErr := json.Unmarshal(buf.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_argument" {
		t.Fatalf("error = %+v", envelope.Error)
	}
	if len(envelope.RecommendedActions) != 0 {
		t.Fatalf("recommended actions = %+v, want none", envelope.RecommendedActions)
	}
}

func TestWriteJSONErrorClassifiesUnknownGroupAPIError(t *testing.T) {
	var buf bytes.Buffer
	err := codedTestError{
		code:    "unknown_group",
		message: "unknown group: typo; available groups: backend, frontend",
	}
	if writeErr := WriteJSONError(&buf, "orbit up --group typo --json", err); writeErr != nil {
		t.Fatalf("WriteJSONError: %v", writeErr)
	}
	var envelope JSONEnvelope
	if decodeErr := json.Unmarshal(buf.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_argument" {
		t.Fatalf("error = %+v", envelope.Error)
	}
	if len(envelope.RecommendedActions) != 0 {
		t.Fatalf("recommended actions = %+v, want none", envelope.RecommendedActions)
	}
}

func TestWriteJSONErrorKeepsProjectSwitchAsSoleRecovery(t *testing.T) {
	var buf bytes.Buffer
	err := WithJSONActions(codedTestError{
		code:    "project_context_inactive",
		message: "project-b is not running; project-a is still active",
	}, []JSONAction{{
		Command: "orbit up --json",
		Reason:  "Stop project-a and start project-b.",
	}})
	if writeErr := WriteJSONError(&buf, "orbit down --json", err); writeErr != nil {
		t.Fatalf("WriteJSONError: %v", writeErr)
	}
	var envelope JSONEnvelope
	if decodeErr := json.Unmarshal(buf.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}
	if envelope.Error == nil || envelope.Error.Code != "project_context_inactive" {
		t.Fatalf("error = %+v", envelope.Error)
	}
	if len(envelope.RecommendedActions) != 1 ||
		envelope.RecommendedActions[0].Command != "orbit up --json" {
		t.Fatalf("recommended actions = %+v", envelope.RecommendedActions)
	}
}

type codedTestError struct {
	code    string
	message string
}

func (e codedTestError) Error() string {
	return e.message
}

func (e codedTestError) ErrorCode() string {
	return e.code
}

func TestWriteJSONErrorClassifiesUnknownResource(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit restart missing --json", NewUnknownResourceError("missing"))
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "unknown_resource" {
		t.Fatalf("error = %+v", got.Error)
	}
	if got.Error.Retryable {
		t.Fatal("retryable = true, want false")
	}
	if got.Error.Hint != "Run 'orbit status --json' to list configured resources." {
		t.Fatalf("hint = %q", got.Error.Hint)
	}
}

func TestWriteJSONErrorClassifiesCodedUnknownResource(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(
		&buf,
		"orbit up missing --json",
		fmt.Errorf("up failed: %w", codedTestError{
			code:    "unknown_resource",
			message: "unknown resource: missing",
		}),
	)
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "unknown_resource" {
		t.Fatalf("error = %+v", got.Error)
	}
	if len(got.RecommendedActions) != 1 || got.RecommendedActions[0].Command != "orbit status --json" {
		t.Fatalf("recommended actions = %+v", got.RecommendedActions)
	}
}

func TestWriteJSONErrorClassifiesTimeout(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit up worker --json", NewTimeoutError("timeout waiting for services to become healthy"))
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "timeout" {
		t.Fatalf("error = %+v", got.Error)
	}
	if !got.Error.Retryable {
		t.Fatal("retryable = false, want true")
	}
}

func TestWriteJSONErrorClassifiesDependencyBlocked(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit restart api --json", NewDependencyBlockedError("api is blocked by redis"))
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "dependency_blocked" {
		t.Fatalf("error = %+v", got.Error)
	}
	if !got.Error.Retryable {
		t.Fatal("retryable = false, want true")
	}
}

func TestWriteJSONErrorClassifiesServiceStartFailed(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit restart api --json", NewServiceStartFailedError("api failed: address already in use"))
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "service_start_failed" {
		t.Fatalf("error = %+v", got.Error)
	}
	if !got.Error.Retryable {
		t.Fatal("retryable = false, want true")
	}
}

func TestWriteJSONErrorClassifiesResourcePortConflict(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit up --json", &ResourcePortConflictError{
		Port:           26379,
		Resource:       "redis",
		PID:            "42",
		Process:        "/usr/bin/redis-server",
		InspectCommand: "ps -p 42 -o pid,comm,args=",
	})
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "resource_port_conflict" ||
		got.Error.NextCommand != "ps -p 42 -o pid,comm,args=" {
		t.Fatalf("error = %+v", got.Error)
	}
	if len(got.RecommendedActions) != 1 ||
		got.RecommendedActions[0].Command != "ps -p 42 -o pid,comm,args=" {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestWriteJSONErrorClassifiesDashboardPortConflict(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit up --json", &daemon.PortConflictError{
		Port:          19800,
		PID:           123,
		SuggestedPort: 29800,
		Err:           fmt.Errorf("bind"),
	})
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "dashboard_port_conflict" {
		t.Fatalf("error = %+v", got.Error)
	}
	if !got.Error.Retryable || !strings.Contains(got.Error.NextCommand, "ORBIT_DASHBOARD_PORT=29800") {
		t.Fatalf("error recovery = %+v", got.Error)
	}
}

func TestWriteJSONFailurePreservesChecks(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]any{"checks": []string{"Daemon"}}
	err := NewChecksFailedError("doctor found 1 failed check: Daemon")
	actions := []JSONAction{{Command: "orbit logs api", Reason: "Inspect the failed service."}}

	if writeErr := WriteJSONFailure(&buf, "orbit doctor --json", data, err, actions); writeErr != nil {
		t.Fatalf("WriteJSONFailure: %v", writeErr)
	}

	got := decodeEnvelope(t, buf.Bytes())
	if got.OK {
		t.Fatal("ok = true, want false")
	}
	if got.Data == nil {
		t.Fatal("data missing from failure")
	}
	if got.Error == nil || got.Error.Code != "checks_failed" {
		t.Fatalf("error = %+v", got.Error)
	}
	foundLogs := false
	for _, action := range got.RecommendedActions {
		if action.Command == "orbit logs api" {
			foundLogs = true
		}
	}
	if !foundLogs {
		t.Fatalf("recommended_actions = %+v, want orbit logs api", got.RecommendedActions)
	}
}

func TestWriteJSONUnsupportedDestructiveCommandEnvelope(t *testing.T) {
	var buf bytes.Buffer
	err := NewUnsupportedDestructiveJSONCommandError(
		"orbit sqlserver publish AppDB",
		"Run manually only after confirming local data loss is acceptable.",
	)

	if writeErr := WriteJSONError(&buf, "orbit sqlserver publish AppDB --json", err); writeErr != nil {
		t.Fatalf("WriteJSONError: %v", writeErr)
	}

	got := decodeEnvelope(t, buf.Bytes())
	if got.OK {
		t.Fatal("ok = true, want false")
	}
	if got.Error == nil || got.Error.Code != "json_unsupported_destructive_command" {
		t.Fatalf("error = %+v", got.Error)
	}
	if len(got.RecommendedActions) != 1 {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
	action := got.RecommendedActions[0]
	if action.Command != "orbit sqlserver publish AppDB" {
		t.Fatalf("recommended command = %q", action.Command)
	}
	if !action.Destructive {
		t.Fatal("recommended action destructive = false, want true")
	}
}

func TestWriteJSONNotConfiguredRequiresAnEditInsteadOfDiagnosticLoop(t *testing.T) {
	var buf bytes.Buffer
	err := NewNotConfiguredError("feature is not enabled; edit the active configuration")
	if writeErr := WriteJSONError(&buf, "orbit feature list --json", err); writeErr != nil {
		t.Fatalf("WriteJSONError: %v", writeErr)
	}

	var got JSONEnvelope
	if decodeErr := json.Unmarshal(buf.Bytes(), &got); decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}
	if got.Error == nil || got.Error.Code != "not_configured" {
		t.Fatalf("error = %+v", got.Error)
	}
	if got.Error.NextCommand != "" || len(got.RecommendedActions) != 0 {
		t.Fatalf("not-configured edit invented diagnostic actions: error=%+v actions=%+v", got.Error, got.RecommendedActions)
	}
}
