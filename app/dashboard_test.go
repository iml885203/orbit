package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/iml885203/orbit/cli"
)

func TestWriteOpenJSONUsesStableEnvelope(t *testing.T) {
	var buf bytes.Buffer
	err := writeOpenJSON(&buf, "orbit open api --json", openJSONData{
		URL:     "http://localhost:8080",
		Target:  "service",
		Service: "api",
		Opened:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got cli.JSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.SchemaVersion != cli.SchemaVersion || got.Command != "orbit open api --json" {
		t.Fatalf("envelope = %+v", got)
	}
	data, ok := got.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want object", got.Data)
	}
	if data["url"] != "http://localhost:8080" || data["target"] != "service" || data["service"] != "api" || data["opened"] != true {
		t.Fatalf("open data = %+v", data)
	}
}
