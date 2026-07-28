package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

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
	if got.Error.NextCommand != "orbit status --json" {
		t.Fatalf("next_command = %q", got.Error.NextCommand)
	}
}

func TestWriteJSONErrorClassifiesConfigMismatch(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit db list --json", &daemon.ConfigMismatchError{
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
	err := WriteJSONError(&buf, "orbit db list --json", &daemon.ConfigMismatchError{
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
	if !got.Error.Retryable || got.Error.NextCommand != "orbit daemon restart --json" {
		t.Fatalf("error recovery = %+v", got.Error)
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
		"orbit db publish AppDB",
		"Run manually only after confirming local data loss is acceptable.",
	)

	if writeErr := WriteJSONError(&buf, "orbit db publish AppDB --json", err); writeErr != nil {
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
	if action.Command != "orbit db publish AppDB" {
		t.Fatalf("recommended command = %q", action.Command)
	}
	if !action.Destructive {
		t.Fatal("recommended action destructive = false, want true")
	}
}
