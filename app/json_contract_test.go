package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
)

func decodeEnvelope(t *testing.T, raw []byte) cli.JSONEnvelope {
	t.Helper()
	var got cli.JSONEnvelope
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, raw)
	}
	return got
}

func TestWriteCLIJSONServiceFailureKeepsRecoveryActionsFocused(t *testing.T) {
	var buf bytes.Buffer
	err := cli.WithJSONActions(
		cli.NewServiceStartFailedError("worker degraded"),
		lifecycleRecommendedActions([]string{"worker"}),
	)
	if err := cli.WriteJSONError(&buf, "orbit up worker --json", err); err != nil {
		t.Fatalf("cli.WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	want := []string{
		"orbit status --json",
		"orbit logs worker --json",
		"orbit restart worker --json",
	}
	if len(got.RecommendedActions) != len(want) {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
	for i, command := range want {
		if got.RecommendedActions[i].Command != command {
			t.Fatalf("recommended_actions[%d] = %q, want %q", i, got.RecommendedActions[i].Command, command)
		}
	}
}

func TestEnvRepoAccessJSONPointsToAuthenticationAndRetry(t *testing.T) {
	var buf bytes.Buffer
	err := cli.NewEnvRepoAccessError("cannot access environment repo https://github.com/example/private.git")
	if err := cli.WriteJSONError(&buf, "orbit env sync --json", err); err != nil {
		t.Fatal(err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "env_repo_access" {
		t.Fatalf("error = %+v", got.Error)
	}
	if !strings.Contains(got.Error.Hint, "gh auth login") {
		t.Fatalf("hint = %q", got.Error.Hint)
	}
	if len(got.RecommendedActions) != 1 || got.RecommendedActions[0].Command != "orbit env sync --json" {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestWriteLogJSONErrorEvent(t *testing.T) {
	var buf bytes.Buffer
	if err := writeLogJSONErrorEvent(&buf, "redis", errors.New("stream disconnected")); err != nil {
		t.Fatalf("writeLogJSONErrorEvent: %v", err)
	}

	var got struct {
		SchemaVersion string `json:"schema_version"`
		Type          string `json:"type"`
		Resource      string `json:"resource"`
		Error         struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode event: %v\n%s", err, buf.String())
	}
	if got.SchemaVersion != cli.SchemaVersion {
		t.Fatalf("schema_version = %q", got.SchemaVersion)
	}
	if got.Type != "error" {
		t.Fatalf("type = %q", got.Type)
	}
	if got.Resource != "redis" {
		t.Fatalf("resource = %q", got.Resource)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &fields); err != nil {
		t.Fatalf("decode event fields: %v", err)
	}
	if _, ok := fields["service"]; ok {
		t.Fatalf("event contains legacy service field: %s", buf.String())
	}
	if got.Error.Code != "log_stream_error" {
		t.Fatalf("error code = %q", got.Error.Code)
	}
	if got.Error.Message != "stream disconnected" {
		t.Fatalf("error message = %q", got.Error.Message)
	}
}

func TestPrintExecutionErrorJSON(t *testing.T) {
	origJSON := cli.JSONOutput
	origArgs := os.Args
	t.Cleanup(func() {
		cli.JSONOutput = origJSON
		os.Args = origArgs
	})
	cli.JSONOutput = true
	os.Args = []string{"orbit", "restart", "missing", "--json"}

	var buf bytes.Buffer
	printExecutionError(&buf, cli.NewUnknownResourceError("missing"))

	got := decodeEnvelope(t, buf.Bytes())
	if got.OK {
		t.Fatal("ok = true, want false")
	}
	if got.Command != "orbit restart missing --json" {
		t.Fatalf("command = %q", got.Command)
	}
	if got.Error == nil || got.Error.Code != "unknown_resource" {
		t.Fatalf("error = %+v", got.Error)
	}
}

func TestPrintExecutionErrorHuman(t *testing.T) {
	origJSON := cli.JSONOutput
	t.Cleanup(func() { cli.JSONOutput = origJSON })
	cli.JSONOutput = false

	var buf bytes.Buffer
	printExecutionError(&buf, cli.NewUnknownResourceError("missing"))

	if got := buf.String(); got != "Error: unknown resource: missing\n" {
		t.Fatalf("human error = %q", got)
	}
}

func TestPrintExecutionErrorHumanExplainsPortRecovery(t *testing.T) {
	origJSON := cli.JSONOutput
	t.Cleanup(func() { cli.JSONOutput = origJSON })
	cli.JSONOutput = false

	var buf bytes.Buffer
	printExecutionError(&buf, &cli.ResourcePortConflictError{
		Port:           26379,
		Resource:       "redis",
		InspectCommand: "lsof -nP -iTCP:26379 -sTCP:LISTEN",
	})

	output := buf.String()
	for _, evidence := range []string{"redis", "26379", "Inspect owner", "lsof", "then run: orbit up"} {
		if !strings.Contains(output, evidence) {
			t.Fatalf("human error missing %q: %s", evidence, output)
		}
	}
	if strings.Contains(output, "orbit logs") || strings.Contains(output, "orbit restart") {
		t.Fatalf("human port recovery recommends blind retry: %s", output)
	}
}

func TestPrintExecutionErrorHumanExplainsNoLogsRecovery(t *testing.T) {
	origJSON := cli.JSONOutput
	t.Cleanup(func() { cli.JSONOutput = origJSON })
	cli.JSONOutput = false

	var buf bytes.Buffer
	err := cli.WithJSONActions(
		cli.NewLogsUnavailableError("no logs for api: the resource did not start"),
		[]cli.JSONAction{{Command: "orbit up api --json"}},
	)
	printExecutionError(&buf, err)

	for _, evidence := range []string{"no logs for api", "Next: orbit up api"} {
		if !strings.Contains(buf.String(), evidence) {
			t.Fatalf("human error missing %q: %s", evidence, buf.String())
		}
	}
}

func TestLogsUnavailableJSONUsesTargetedRecoveryOnly(t *testing.T) {
	var buf bytes.Buffer
	err := cli.WithJSONActions(
		cli.NewLogsUnavailableError("no logs for api: the resource did not start"),
		[]cli.JSONAction{{
			Command: "orbit up api --json",
			Reason:  "Retry api.",
		}},
	)
	if err := cli.WriteJSONError(&buf, "orbit logs api --json", err); err != nil {
		t.Fatal(err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "logs_unavailable" {
		t.Fatalf("error = %+v", got.Error)
	}
	if len(got.RecommendedActions) != 1 || got.RecommendedActions[0].Command != "orbit up api --json" {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestWorkingDirectoryJSONUsesExactRecoveryOnly(t *testing.T) {
	var buf bytes.Buffer
	err := cli.WithJSONReplacementActions(
		cli.NewServiceWorkingDirectoryError("working directory not found"),
		[]cli.JSONAction{{
			Command: `orbit settings set workspace-root "$PWD" --json`,
			Reason:  "Set the workspace root.",
		}},
	)
	if err := cli.WriteJSONError(&buf, "orbit up --json", err); err != nil {
		t.Fatal(err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "service_working_directory_missing" {
		t.Fatalf("error = %+v", got.Error)
	}
	if len(got.RecommendedActions) != 1 || got.RecommendedActions[0].Command != `orbit settings set workspace-root "$PWD" --json` {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestPrintExecutionErrorSkipsAlreadyRenderedJSONError(t *testing.T) {
	origJSON := cli.JSONOutput
	t.Cleanup(func() { cli.JSONOutput = origJSON })
	cli.JSONOutput = true

	var buf bytes.Buffer
	printExecutionError(&buf, errCLIJSONAlreadyRendered{err: errors.New("stream disconnected")})

	if got := buf.String(); got != "" {
		t.Fatalf("already-rendered error output = %q, want empty", got)
	}
}
