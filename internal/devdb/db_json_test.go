package devdb

import (
	"bytes"
	"encoding/json"
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

func TestDBPublishJSONRejectsInvalidNameBeforeDestructiveRecommendation(t *testing.T) {
	origJSON := cli.JSONOutput
	t.Cleanup(func() { cli.JSONOutput = origJSON })
	cli.JSONOutput = true

	err := runDBPublish(nil, []string{"bad;name"})
	if err == nil {
		t.Fatal("runDBPublish returned nil, want invalid database error")
	}

	var buf bytes.Buffer
	if writeErr := cli.WriteJSONError(&buf, "orbit sqlserver publish bad;name --json", err); writeErr != nil {
		t.Fatalf("cli.WriteJSONError: %v", writeErr)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code == "json_unsupported_destructive_command" {
		t.Fatalf("invalid name must be reported as an input error, not the destructive recommendation: %+v", got.Error)
	}
}
