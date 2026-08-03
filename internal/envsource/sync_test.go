package envsource

import (
	"os"
	"path/filepath"
	"testing"
)

const validEnvironment = `version: "3"
services:
  api:
    kind: backend
    command: python3 -m http.server 8080
`

func TestLocalSyncReplacesCacheAndIncludesUncommittedFiles(t *testing.T) {
	home := t.TempDir()
	sourceRoot := t.TempDir()
	envs := filepath.Join(sourceRoot, "envs")
	if err := os.MkdirAll(envs, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envs, "e2e.yaml"), []byte(validEnvironment), 0644); err != nil {
		t.Fatal(err)
	}
	source := Source{Name: "env-dev", Type: TypeLocal, Path: sourceRoot}
	if _, err := Sync(source, home, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(envs, "e2e.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envs, "local.yaml"), []byte(validEnvironment), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(source, home, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(EnvsDir(home, source.Name), "e2e.yaml")); !os.IsNotExist(err) {
		t.Fatalf("removed environment remains in cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(EnvsDir(home, source.Name), "local.yaml")); err != nil {
		t.Fatalf("uncommitted local environment missing: %v", err)
	}
}

func TestFailedSyncPreservesLastValidCache(t *testing.T) {
	home := t.TempDir()
	sourceRoot := t.TempDir()
	envs := filepath.Join(sourceRoot, "envs")
	if err := os.MkdirAll(envs, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(envs, "e2e.yaml")
	if err := os.WriteFile(path, []byte(validEnvironment), 0644); err != nil {
		t.Fatal(err)
	}
	source := Source{Name: "env-dev", Type: TypeLocal, Path: sourceRoot}
	if _, err := Sync(source, home, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("version: ["), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(source, home, false); err == nil {
		t.Fatal("invalid source sync succeeded")
	}
	data, err := os.ReadFile(filepath.Join(EnvsDir(home, source.Name), "e2e.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != validEnvironment {
		t.Fatalf("last valid cache changed to %q", data)
	}
}
