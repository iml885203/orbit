package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The inline Extensions map disables yaml.v3's KnownFields for top-level
// keys — the allowlist check must keep typo'd keys failing loudly (the
// strict-decoding regression the spec calls out).
func TestLoad_UnknownTopLevelKeyStillFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	yaml := "version: \"3\"\nservcies:\n  api:\n    type: node\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("typo'd top-level key loaded silently")
	}
	if !strings.Contains(err.Error(), "servcies") {
		t.Fatalf("error does not name the offending key: %v", err)
	}
	if !strings.Contains(err.Error(), `did you mean "services"`) {
		t.Fatalf("error does not correct the top-level key: %v", err)
	}
}

// Unknown keys nested inside core sections still trip KnownFields — the
// inline map only absorbs top level.
func TestLoad_UnknownNestedKeyStillFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	yaml := "version: \"3\"\ncontainers:\n  redis:\n    image: redis:7.4\n    imagee: typo\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("typo'd nested key loaded silently")
	} else if !strings.Contains(err.Error(), `did you mean "image"`) {
		t.Fatalf("error does not correct the nested key: %v", err)
	}
}

func TestLoad_RejectsRemovedPreviewOnlyField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	yaml := "version: \"3\"\npreviewOnly: true\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "previewOnly was removed; delete this field") {
		t.Fatalf("Load() error = %v, want previewOnly migration guidance", err)
	}
}

func TestDecodeStrictSuggestsExtensionField(t *testing.T) {
	type section struct {
		Endpoint string `yaml:"endpoint"`
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("endpont: http://localhost\n"), &node); err != nil {
		t.Fatal(err)
	}
	var out section

	err := DecodeStrict(node.Content[0], &out)

	if err == nil || !strings.Contains(err.Error(), `did you mean "endpoint"`) {
		t.Fatalf("DecodeStrict() = %v, want extension field suggestion", err)
	}
}

func TestLoadIgnoresXPrefixedTopLevelKeys(t *testing.T) {
	path := writeOrbitYAML(t, `version: "3"
x-notes:
  owner: platform
  anything: [goes, here]
x-empty:
services:
  api:
    type: node
    command: npm start
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for name := range cfg.Extensions {
		if strings.HasPrefix(name, "x-") {
			t.Fatalf("x- key %q leaked into the loaded config", name)
		}
	}
	if cfg.Extension("x-notes") != nil {
		t.Fatal("x-notes decoded as an extension section")
	}
}

func TestLoadSharesXAnchorsThroughMergeKeys(t *testing.T) {
	path := writeOrbitYAML(t, `version: "3"
x-dotnet-env: &dotnet-env
  ASPNETCORE_ENVIRONMENT: Development
  OTEL_DOTNET_EXPERIMENTAL_OTLP_EMIT_EXCEPTION_LOG_ATTRIBUTES: "true"

services:
  api:
    type: dotnet
    command: dotnet run
    env:
      <<: *dotnet-env
      HTTP_PORTS: "8080"
  worker:
    type: dotnet
    command: dotnet run
    env:
      <<: *dotnet-env
      ASPNETCORE_ENVIRONMENT: Staging
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantAPI := map[string]string{
		"ASPNETCORE_ENVIRONMENT": "Development",
		"OTEL_DOTNET_EXPERIMENTAL_OTLP_EMIT_EXCEPTION_LOG_ATTRIBUTES": "true",
		"HTTP_PORTS": "8080",
	}
	if got := cfg.Services["api"].Env; !reflect.DeepEqual(got, wantAPI) {
		t.Fatalf("api env = %#v, want %#v", got, wantAPI)
	}
	if got := cfg.Services["worker"].Env["ASPNETCORE_ENVIRONMENT"]; got != "Staging" {
		t.Fatalf("worker ASPNETCORE_ENVIRONMENT = %q, want explicit key to win over the anchor", got)
	}
}

func TestLoadRejectsUnknownTopLevelKeyWithoutXPrefix(t *testing.T) {
	path := writeOrbitYAML(t, "version: \"3\"\nx_shared:\n  LOG_LEVEL: debug\n")

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown top-level section x_shared") {
		t.Fatalf("Load error = %v, want unknown section", err)
	}
	if !strings.Contains(err.Error(), "keys starting with x- are ignored") {
		t.Fatalf("Load error = %v, want x- guidance", err)
	}
}
