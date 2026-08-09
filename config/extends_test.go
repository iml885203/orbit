package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const parentEnvYAML = `
version: "3"
containers:
  redis:
    image: redis:7.4
    ports:
      redis: 6379
    volumes:
      - team_redis:/data
    health_check:
      type: tcp
services:
  api:
    type: dotnet
    path: /tmp/api.csproj
    ports:
      http: 5000
    env:
      ASPNETCORE_ENVIRONMENT: Development
      ConnectionStrings__Main: "Server=localhost"
    depends_on: [redis]
`

func writeEnvFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestExtends_NamedKeysReplaceUnnamedInherit(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		"backoffice.yaml": parentEnvYAML,
		"e2e.yaml": `
extends: backoffice.yaml
services:
  api:
    env:
      ASPNETCORE_ENVIRONMENT: Docker
`,
	})

	cfg, err := Load(filepath.Join(dir, "e2e.yaml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	api := cfg.Services["api"]
	if got := api.Env["ASPNETCORE_ENVIRONMENT"]; got != "Docker" {
		t.Errorf("named env key = %q, want overridden %q", got, "Docker")
	}
	if got := api.Env["ConnectionStrings__Main"]; got != "Server=localhost" {
		t.Errorf("unnamed sibling env key = %q, want inherited %q", got, "Server=localhost")
	}
	if api.Path != "/tmp/api.csproj" {
		t.Errorf("unnamed service field path = %q, want inherited", api.Path)
	}
	if _, ok := cfg.Containers["redis"]; !ok {
		t.Errorf("unnamed container redis not inherited")
	}
	if cfg.Version != "3" {
		t.Errorf("version = %q, want inherited %q", cfg.Version, "3")
	}
}

func TestExtends_SequenceReplacesWholesale(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		"backoffice.yaml": parentEnvYAML,
		"e2e.yaml": `
extends: backoffice.yaml
containers:
  redis:
    volumes:
      - e2e_redis:/data
`,
	})

	cfg, err := Load(filepath.Join(dir, "e2e.yaml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	got := cfg.Containers["redis"].Volumes
	if len(got) != 1 || got[0] != "e2e_redis:/data" {
		t.Errorf("volumes = %v, want the child's list to replace the parent's", got)
	}
}

func TestExtends_ChildOnlyKeysAdded(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		"backoffice.yaml": parentEnvYAML,
		"e2e.yaml": `
extends: backoffice.yaml
containers:
  mongo:
    image: mongo:6.0.8
    ports:
      mongo: 27017
`,
	})

	cfg, err := Load(filepath.Join(dir, "e2e.yaml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if _, ok := cfg.Containers["mongo"]; !ok {
		t.Errorf("child-only container mongo missing from merge")
	}
	if _, ok := cfg.Containers["redis"]; !ok {
		t.Errorf("parent container redis lost when child adds a sibling")
	}
}

func TestExtends_ParentInSubdirectory(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		"base/backoffice.yaml": parentEnvYAML,
		"e2e.yaml": `
extends: base/backoffice.yaml
services:
  api:
    env:
      ASPNETCORE_ENVIRONMENT: Docker
`,
	})

	cfg, err := Load(filepath.Join(dir, "e2e.yaml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got := cfg.Services["api"].Env["ASPNETCORE_ENVIRONMENT"]; got != "Docker" {
		t.Errorf("override via subdirectory parent = %q, want %q", got, "Docker")
	}
}

func TestExtends_EnvSubstitutionPerFile(t *testing.T) {
	t.Setenv("EXTENDS_TEST_IMAGE", "registry.example/redis:7.4")
	dir := writeEnvFiles(t, map[string]string{
		"backoffice.yaml": `
version: "3"
containers:
  redis:
    image: ${EXTENDS_TEST_IMAGE:-redis:7.4}
    ports:
      redis: 6379
`,
		"e2e.yaml": `
extends: backoffice.yaml
containers:
  redis:
    ports:
      redis: 16379
`,
	})

	cfg, err := Load(filepath.Join(dir, "e2e.yaml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	redis := cfg.Containers["redis"]
	if redis.Image != "registry.example/redis:7.4" {
		t.Errorf("parent ${VAR} not substituted independently, image = %q", redis.Image)
	}
	if redis.Ports["redis"].Host != 16379 {
		t.Errorf("child port override lost, port = %d", redis.Ports["redis"].Host)
	}
}

func TestExtends_MissingParentFails(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		"e2e.yaml": "extends: nowhere.yaml\n",
	})

	_, err := Load(filepath.Join(dir, "e2e.yaml"))
	if err == nil {
		t.Fatal("expected error for missing parent")
	}
	if !strings.Contains(err.Error(), "nowhere.yaml") {
		t.Errorf("error should name the extends target, got: %v", err)
	}
}

func TestExtends_ChainRejected(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		"grandparent.yaml": parentEnvYAML,
		"parent.yaml":      "extends: grandparent.yaml\n",
		"child.yaml":       "extends: parent.yaml\n",
	})

	_, err := Load(filepath.Join(dir, "child.yaml"))
	if err == nil {
		t.Fatal("expected error for extends chain")
	}
	if !strings.Contains(err.Error(), "single-level") {
		t.Errorf("error should explain the single-level contract, got: %v", err)
	}
}

func TestExtends_SelfReferenceRejected(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		"loop.yaml": "extends: loop.yaml\n",
	})

	_, err := Load(filepath.Join(dir, "loop.yaml"))
	if err == nil {
		t.Fatal("expected error for self-extends")
	}
	if !strings.Contains(err.Error(), "extend itself") {
		t.Errorf("error should call out the self-reference, got: %v", err)
	}
}

func TestExtends_AbsolutePathRejected(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		"backoffice.yaml": `version: "3"`,
	})
	parentPath := filepath.Join(dir, "backoffice.yaml")
	childPath := filepath.Join(dir, "e2e.yaml")
	if err := os.WriteFile(childPath, []byte("extends: "+parentPath+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(childPath)
	if err == nil {
		t.Fatal("Load succeeded, want absolute extends path error")
	}
	if !strings.Contains(err.Error(), "must be a path relative") {
		t.Fatalf("error = %q, want relative-path guidance", err)
	}
}

func TestExtends_MergedResultStillValidated(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		"backoffice.yaml": parentEnvYAML,
		"e2e.yaml": `
extends: backoffice.yaml
services:
  api:
    depends_on: [ghost]
`,
	})

	_, err := Load(filepath.Join(dir, "e2e.yaml"))
	if err == nil {
		t.Fatal("expected validation error for dangling dependency in merged config")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("validation should see the merged config, got: %v", err)
	}
}

func TestExtends_UnknownTopLevelKeyStillFails(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		"backoffice.yaml": parentEnvYAML,
		"e2e.yaml": `
extends: backoffice.yaml
servicess:
  api: {}
`,
	})

	_, err := Load(filepath.Join(dir, "e2e.yaml"))
	if err == nil {
		t.Fatal("expected error for typo'd top-level key")
	}
	if !strings.Contains(err.Error(), "servicess") {
		t.Errorf("error should name the unknown key, got: %v", err)
	}
}
