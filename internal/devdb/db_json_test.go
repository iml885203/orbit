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

func TestDataPreservingPublishJSONReturnsStableSuccess(t *testing.T) {
	originalJSON, originalForce, originalAll, originalParallel := cli.JSONOutput, publishForce, publishAll, publishParallel
	t.Cleanup(func() {
		cli.JSONOutput, publishForce, publishAll, publishParallel = originalJSON, originalForce, originalAll, originalParallel
	})
	cli.JSONOutput, publishForce, publishAll, publishParallel = true, false, false, 0

	if err := rejectForcedPublishJSON([]string{"AppDB"}); err != nil {
		t.Fatalf("ordinary publish rejected as destructive: %v", err)
	}
	var buf bytes.Buffer
	if err := writeDBPublishJSONTo(&buf, []string{"AppDB"}, []string{"AppDB"}); err != nil {
		t.Fatalf("writeDBPublishJSONTo: %v", err)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if !got.OK || got.Command != "orbit sqlserver publish AppDB --json" {
		t.Fatalf("envelope = %+v", got)
	}
	if len(got.RecommendedActions) != 0 {
		t.Fatalf("successful publish invented actions: %+v", got.RecommendedActions)
	}
	data, ok := got.Data.(map[string]any)
	if !ok || data["published"] != float64(1) || data["data_loss_allowed"] != false {
		t.Fatalf("publish data = %#v", got.Data)
	}
}

func TestForcedPublishJSONPreservesDestructiveIntentForManualApproval(t *testing.T) {
	originalJSON, originalForce, originalYes, originalAll, originalParallel := cli.JSONOutput, publishForce, publishYes, publishAll, publishParallel
	t.Cleanup(func() {
		cli.JSONOutput, publishForce, publishYes, publishAll, publishParallel = originalJSON, originalForce, originalYes, originalAll, originalParallel
	})
	cli.JSONOutput, publishForce, publishYes, publishAll, publishParallel = true, true, true, true, 4

	err := rejectForcedPublishJSON(nil)
	if err == nil {
		t.Fatal("forced publish unexpectedly accepted JSON mode")
	}
	var buf bytes.Buffer
	if writeErr := cli.WriteJSONError(&buf, "orbit sqlserver publish --all --parallel=4 --force --yes --json", err); writeErr != nil {
		t.Fatalf("WriteJSONError: %v", writeErr)
	}
	got := decodeEnvelope(t, buf.Bytes())
	if got.Error == nil || got.Error.Code != "json_unsupported_destructive_command" {
		t.Fatalf("error = %+v", got.Error)
	}
	if len(got.RecommendedActions) != 1 {
		t.Fatalf("actions = %+v", got.RecommendedActions)
	}
	action := got.RecommendedActions[0]
	if action.Command != "orbit sqlserver publish --all --parallel=4 --allow-data-loss" || !action.Destructive {
		t.Fatalf("manual replacement = %+v", action)
	}
}
