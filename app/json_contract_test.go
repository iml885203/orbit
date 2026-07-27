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

func TestWriteCLIJSONErrorMergesActionErrorRecommendations(t *testing.T) {
	var buf bytes.Buffer
	err := cli.WithJSONActions(errors.New("worker degraded"), lifecycleRecommendedActions([]string{"worker"}))
	if err := cli.WriteJSONError(&buf, "orbit up worker --json", err); err != nil {
		t.Fatalf("cli.WriteJSONError: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	want := "orbit logs worker --json"
	for _, action := range got.RecommendedActions {
		if action.Command == want {
			return
		}
	}
	t.Fatalf("missing %q in %+v", want, got.RecommendedActions)
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
		Service       string `json:"service"`
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
	if got.Service != "redis" {
		t.Fatalf("service = %q", got.Service)
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
	printExecutionError(&buf, cli.NewUnknownServiceError("missing"))

	got := decodeEnvelope(t, buf.Bytes())
	if got.OK {
		t.Fatal("ok = true, want false")
	}
	if got.Command != "orbit restart missing --json" {
		t.Fatalf("command = %q", got.Command)
	}
	if got.Error == nil || got.Error.Code != "unknown_service" {
		t.Fatalf("error = %+v", got.Error)
	}
}

func TestPrintExecutionErrorHuman(t *testing.T) {
	origJSON := cli.JSONOutput
	t.Cleanup(func() { cli.JSONOutput = origJSON })
	cli.JSONOutput = false

	var buf bytes.Buffer
	printExecutionError(&buf, cli.NewUnknownServiceError("missing"))

	if got := buf.String(); got != "Error: unknown service: missing\n" {
		t.Fatalf("human error = %q", got)
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
