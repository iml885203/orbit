package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestWriteJSONErrorClassifiesUnknownService(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "orbit restart missing --json", NewUnknownServiceError("missing"))
	if err != nil {
		t.Fatalf("WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "unknown_service" {
		t.Fatalf("error = %+v", got.Error)
	}
	if got.Error.Retryable {
		t.Fatal("retryable = true, want false")
	}
	if got.Error.Hint != "Run 'orbit status --json' to list configured services." {
		t.Fatalf("hint = %q", got.Error.Hint)
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
