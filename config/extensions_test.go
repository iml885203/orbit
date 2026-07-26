package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The inline Extensions map disables yaml.v3's KnownFields for top-level
// keys — the allowlist check must keep typo'd keys failing loudly (the
// strict-decoding regression the spec calls out).
func TestLoad_UnknownTopLevelKeyStillFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	yaml := "version: \"2\"\nservcies:\n  api:\n    type: node\n"
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
}

// Unknown keys nested inside core sections still trip KnownFields — the
// inline map only absorbs top level.
func TestLoad_UnknownNestedKeyStillFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	yaml := "version: \"2\"\ncontainers:\n  redis:\n    image: redis:7.4\n    imagee: typo\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("typo'd nested key loaded silently")
	}
}
