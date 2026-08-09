package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnvFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadInheritsAndOverridesNamedValues(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir, "base.yaml", `
version: "3"
settings:
  shutdown_timeout: 20s
  health_check_interval: 4s
containers:
  redis:
    image: redis:7
services:
  api:
    type: node
    path: ./base-api
    command: npm start
    env:
      LOG_LEVEL: info
      REGION: local
  worker:
    type: node
    command: npm run worker
`)
	childPath := writeEnvFile(t, dir, "e2e.yaml", `
extends: base.yaml
settings:
  shutdown_timeout: 5s
services:
  api:
    path: ./e2e-api
    env:
      LOG_LEVEL: debug
`)

	cfg, err := Load(childPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != "3" || cfg.Containers["redis"] == nil || cfg.Services["worker"] == nil {
		t.Fatalf("unnamed parent values were not inherited: %#v", cfg)
	}
	if got := cfg.Settings.ShutdownTimeout.String(); got != "5s" {
		t.Fatalf("shutdown timeout = %s, want 5s", got)
	}
	if got := cfg.Settings.HealthCheckInterval.String(); got != "4s" {
		t.Fatalf("health interval = %s, want 4s", got)
	}
	if got := cfg.Services["api"].Path; got != filepath.Join(dir, "e2e-api") {
		t.Fatalf("api path = %q, want child override", got)
	}
	if got := cfg.Services["api"].Type; got != "node" {
		t.Fatalf("api type = %q, want inherited node", got)
	}
	if got := cfg.Services["api"].Env["LOG_LEVEL"]; got != "debug" {
		t.Fatalf("LOG_LEVEL = %q, want debug", got)
	}
	if got := cfg.Services["api"].Env["REGION"]; got != "local" {
		t.Fatalf("REGION = %q, want inherited local", got)
	}
}

func TestLoadExtendsPathAndValuesUseEnvironmentSubstitution(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ORBIT_TEST_PARENT", "parents/base.yaml")
	t.Setenv("ORBIT_TEST_IMAGE", "redis:7.4-alpine")
	writeEnvFile(t, dir, "parents/base.yaml", `
version: "3"
containers:
  redis:
    image: ${ORBIT_TEST_IMAGE:-redis:latest}
`)
	childPath := writeEnvFile(t, dir, "e2e.yaml", `
extends: ${ORBIT_TEST_PARENT:-base.yaml}
services:
  api:
    type: node
    command: npm start
`)

	cfg, err := Load(childPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Containers["redis"].Image; got != "redis:7.4-alpine" {
		t.Fatalf("image = %q, want substituted parent value", got)
	}
}

func TestLoadChildListReplacesParentList(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir, "base.yaml", `
version: "3"
services:
  api:
    type: node
    command: npm start
    depends_on: [redis, kafka]
containers:
  redis:
    image: redis:7
  kafka:
    image: apache/kafka:4
`)
	childPath := writeEnvFile(t, dir, "child.yaml", `
extends: base.yaml
services:
  api:
    depends_on: [redis]
`)

	cfg, err := Load(childPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Services["api"].DependsOn; len(got) != 1 || got[0] != "redis" {
		t.Fatalf("depends_on = %#v, want replacement list", got)
	}
}

func TestLoadRejectsMultiLevelExtends(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir, "grandparent.yaml", "version: \"3\"\n")
	writeEnvFile(t, dir, "parent.yaml", "extends: grandparent.yaml\n")
	childPath := writeEnvFile(t, dir, "child.yaml", "extends: parent.yaml\n")

	_, err := Load(childPath)
	if err == nil || !strings.Contains(err.Error(), "extends may only be used one level deep") {
		t.Fatalf("Load error = %v, want single-level guidance", err)
	}
}

func TestLoadRejectsInvalidExtendsAndMissingParent(t *testing.T) {
	t.Run("non-string", func(t *testing.T) {
		path := writeOrbitYAML(t, "extends: [base.yaml]\n")
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "extends must be a non-empty file path") {
			t.Fatalf("Load error = %v, want extends path guidance", err)
		}
	})

	t.Run("missing parent", func(t *testing.T) {
		path := writeOrbitYAML(t, "extends: missing.yaml\n")
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "loading parent env file") {
			t.Fatalf("Load error = %v, want parent context", err)
		}
	})

	t.Run("absolute parent", func(t *testing.T) {
		path := writeOrbitYAML(t, "extends: "+filepath.ToSlash(filepath.Join(t.TempDir(), "base.yaml"))+"\n")
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "extends must be relative") {
			t.Fatalf("Load error = %v, want relative path guidance", err)
		}
	})
}

func TestLoadRejectsNullAsInheritedDeleteMarker(t *testing.T) {
	tests := []struct {
		name   string
		parent string
		child  string
		path   string
	}{
		{
			name:   "service",
			parent: "services:\n  worker:\n    type: node\n    command: npm start\n",
			child:  "services:\n  worker: null\n",
			path:   "services.worker",
		},
		{
			name:   "container",
			parent: "containers:\n  redis:\n    image: redis:7\n",
			child:  "containers:\n  redis: null\n",
			path:   "containers.redis",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeEnvFile(t, dir, "base.yaml", "version: \"3\"\n"+tt.parent)
			childPath := writeEnvFile(t, dir, "child.yaml", "extends: base.yaml\n"+tt.child)

			_, err := Load(childPath)
			if err == nil || !strings.Contains(err.Error(), "inherited key "+tt.path+" cannot be null") {
				t.Fatalf("Load error = %v, want rejected delete marker", err)
			}
		})
	}
}

func TestLoadInheritedResultStillUsesStrictDecodeAndValidation(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir, "base.yaml", "version: \"3\"\nservices:\n  api:\n    type: node\n    command: npm start\n")
	childPath := writeEnvFile(t, dir, "child.yaml", "extends: base.yaml\nservices:\n  api:\n    typo: value\n")

	_, err := Load(childPath)
	if err == nil || !strings.Contains(err.Error(), "unknown field typo") {
		t.Fatalf("Load error = %v, want strict field error", err)
	}
}

func TestLoadStandaloneSchemaErrorKeepsAuthoredLine(t *testing.T) {
	path := writeOrbitYAML(t, "version: \"3\"\n\nservices:\n  api:\n    type: node\n\n    command: npm start\n    typo: value\n")

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), path+": yaml: unmarshal errors:\n  line 8: unknown field typo") {
		t.Fatalf("Load error = %v, want authored file and line 8", err)
	}
}

func TestLoadParentSchemaErrorNamesParentAndAuthoredLine(t *testing.T) {
	dir := t.TempDir()
	parent := writeEnvFile(t, dir, "base.yaml", "version: \"3\"\n\nservices:\n  api:\n    type: node\n\n    command: npm start\n    typo: value\n")
	child := writeEnvFile(t, dir, "child.yaml", "extends: base.yaml\n")

	_, err := Load(child)
	if err == nil || !strings.Contains(err.Error(), parent+": yaml: unmarshal errors:\n  line 8: unknown field typo") {
		t.Fatalf("Load error = %v, want parent file and authored line 8", err)
	}
}

func TestInheritanceFilesResolvesParentBesideChild(t *testing.T) {
	dir := t.TempDir()
	childPath := writeEnvFile(t, dir, "child.yaml", "extends: parents/base.yaml\n")

	paths, err := InheritanceFiles(childPath)
	if err != nil {
		t.Fatalf("InheritanceFiles: %v", err)
	}
	wantParent := filepath.Join(dir, "parents", "base.yaml")
	if len(paths) != 2 || paths[0] != childPath || paths[1] != wantParent {
		t.Fatalf("paths = %#v, want child and %q", paths, wantParent)
	}
}
