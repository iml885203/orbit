package app

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/iml885203/orbit/cli"
)

func TestVersionCommandMatchesVersionFlagOutput(t *testing.T) {
	originalVersion, originalBuildTime := version, buildTime
	originalJSON := cli.JSONOutput
	t.Cleanup(func() {
		version, buildTime = originalVersion, originalBuildTime
		cli.JSONOutput = originalJSON
	})
	version = "v0.0.4"
	buildTime = ""
	cli.JSONOutput = false

	cmd := versionCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := runVersion(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), buildVersion()+"\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVersionCommandJSONUsesCLIEnvelope(t *testing.T) {
	originalVersion, originalBuildTime := version, buildTime
	originalJSON := cli.JSONOutput
	originalArgs := os.Args
	t.Cleanup(func() {
		version, buildTime = originalVersion, originalBuildTime
		cli.JSONOutput = originalJSON
		os.Args = originalArgs
	})
	version = "v0.0.4"
	buildTime = ""
	cli.JSONOutput = true
	os.Args = []string{"orbit", "version", "--json"}

	cmd := versionCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := runVersion(cmd, nil); err != nil {
		t.Fatal(err)
	}

	var envelope struct {
		SchemaVersion string          `json:"schema_version"`
		OK            bool            `json:"ok"`
		Command       string          `json:"command"`
		Data          versionJSONData `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode output: %v\n%s", err, output.String())
	}
	if envelope.SchemaVersion != cli.SchemaVersion || !envelope.OK {
		t.Fatalf("envelope = %+v", envelope)
	}
	if envelope.Command != "orbit version --json" {
		t.Fatalf("command = %q", envelope.Command)
	}
	if envelope.Data.Version != "v0.0.4" {
		t.Fatalf("version = %q", envelope.Data.Version)
	}
}
