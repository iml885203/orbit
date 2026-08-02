package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMinimalConfig(t *testing.T) {
	content := `
version: "3"
settings:
  shutdown_timeout: 10s
containers:
  redis:
    image: redis:7.4
    icon: simple-icons:redis
    ports:
      redis: 6379
    health_check:
      type: tcp
services:
  api:
    type: dotnet
    path: /tmp/test.csproj
    ports:
      http: 5000
    health_check:
      type: http
      path: /health
    depends_on: [redis]
`
	path := writeOrbitYAML(t, content)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Version != "3" {
		t.Errorf("version = %q, want %q", cfg.Version, "3")
	}
	if len(cfg.Containers) != 1 {
		t.Errorf("containers = %d, want 1", len(cfg.Containers))
	}
	if len(cfg.Services) != 1 {
		t.Errorf("services = %d, want 1", len(cfg.Services))
	}
	if cfg.Containers["redis"].Name != "redis" {
		t.Errorf("container name not populated")
	}
	if cfg.Containers["redis"].PullPolicy != "always" {
		t.Errorf("container pull policy default not applied, got %q", cfg.Containers["redis"].PullPolicy)
	}
	if cfg.Containers["redis"].Icon != "simple-icons:redis" {
		t.Errorf("container icon = %q, want simple-icons:redis", cfg.Containers["redis"].Icon)
	}
	if got := cfg.Containers["redis"].HealthCheck.FailureThreshold; got != DefaultHealthFailureThreshold {
		t.Errorf("failure threshold = %d, want %d", got, DefaultHealthFailureThreshold)
	}
	if got := cfg.Containers["redis"].HealthCheck.Port; got != 6379 {
		t.Errorf("container health port = %d, want inferred 6379", got)
	}
	if cfg.Services["api"].Command != "dotnet watch run" {
		t.Errorf("dotnet default command not applied, got %q", cfg.Services["api"].Command)
	}
	if got := cfg.Services["api"].HealthCheck.Port; got != 5000 {
		t.Errorf("service health port = %d, want inferred 5000", got)
	}
	if got := cfg.Services["api"].ResolveURL(); got != "http://localhost:5000" {
		t.Errorf("service URL = %q, want inferred HTTP endpoint", got)
	}
}

func TestApplyDefaultsUpdatesContainerPointersInMap(t *testing.T) {
	cfg := Config{
		Containers: map[string]*Container{
			"sql-server": {
				Image: "example.db:latest",
			},
		},
	}

	applyDefaults(&cfg)

	if cfg.Containers["sql-server"].PullPolicy != "always" {
		t.Fatalf("pull policy = %q, want always", cfg.Containers["sql-server"].PullPolicy)
	}
}

// resolveRelativePaths should resolve every path-valued field, not just
// Init.TopicsFile, so a yaml living next to its referenced files works
// regardless of which field references them.
func TestLoad_ResolvesRelativeServicePath(t *testing.T) {
	path := writeOrbitYAML(t, `
version: "3"
services:
  api:
    type: node
    path: ./apps/api
    command: pnpm dev
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(filepath.Dir(path), "apps", "api")
	if got := cfg.Services["api"].Path; got != want {
		t.Errorf("service path = %q, want %q (should be resolved relative to yaml dir)", got, want)
	}
}

func TestLoad_ResolvesRelativeSeedFiles(t *testing.T) {
	path := writeOrbitYAML(t, `
version: "3"
containers:
  db:
    image: mongo:8
    seed:
      command: mongosh --quiet appdb
      files:
        - ./seeds/001-users.sql
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(filepath.Dir(path), "seeds", "001-users.sql")
	if got := cfg.Containers["db"].Seed.Files[0]; got != want {
		t.Errorf("seed file = %q, want %q", got, want)
	}
}

func TestEnvVarSubstitution(t *testing.T) {
	// Use an OS-absolute base (a Unix-style "/custom/path" is not absolute on
	// Windows and would be joined to the config dir) so this asserts the
	// substitution itself, not the downstream relative-path resolution.
	base := filepath.ToSlash(t.TempDir())
	t.Setenv("TEST_ORBIT_VAR", base)

	content := `
version: "3"
services:
  api:
    type: node
    path: ${TEST_ORBIT_VAR}/app
    command: node index.js
`
	path := writeOrbitYAML(t, content)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	want := base + "/app"
	if got := filepath.ToSlash(cfg.Services["api"].Path); got != want {
		t.Errorf("env var not substituted, got %q, want %q", got, want)
	}
}

// writeOrbitYAML drops yaml content into a temp dir and returns the path,
// so tests can assert on Load(path) without repeating WriteFile ceremony.
func writeOrbitYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "orbit.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	cases := []struct {
		in, want string
	}{
		{"~/dev/foo", filepath.Join(home, "dev", "foo")},
		{"~", home},
		{"/abs/path", "/abs/path"},
		{"./rel/path", "./rel/path"},
		{"", ""},
		{"~user/dev", "~user/dev"}, // unsupported form left untouched
	}
	for _, c := range cases {
		if got := expandHome(c.in); got != c.want {
			t.Errorf("expandHome(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestLoad_ExpandsHomeInEveryPathField guarantees that home expansion
// reaches every path-valued field — Service.Path, Container.Volumes,
// Container.Init.TopicsFile, Container.Seed.Files, and Sidecar.Volumes.
// One yaml exercises them all so a future refactor that drops any field
// from expandHomePaths fails here, not silently in production.
//
// The ${ORBIT_TEST_MISSING_VAR:-...} dance on Service.Path also covers
// the original reported scenario: yaml fallback expression resolves to
// a literal "~/..." string and must still be expanded.
func TestLoad_ExpandsHomeInEveryPathField(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	t.Setenv("ORBIT_TEST_MISSING_VAR", "")
	_ = os.Unsetenv("ORBIT_TEST_MISSING_VAR")

	path := writeOrbitYAML(t, `
version: "3"
services:
  api:
    type: node
    path: ${ORBIT_TEST_MISSING_VAR:-~/dev/example}/payments
    command: pnpm dev
containers:
  db:
    image: mongo:8
    volumes:
      - ~/db-data:/var/lib/postgresql
    seed:
      command: mongosh --quiet appdb
      files:
        - ~/seeds/001-users.sql
    sidecars:
      - name: admin
        image: dpage/pgadmin4
        volumes:
          - ~/pgadmin-data:/var/lib/pgadmin
  kafka:
    image: confluentinc/cp-kafka:7.9.0
    init:
      type: kafka_topics
      topics_file: ~/topics.yaml
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		field, got, want string
	}{
		{"Service.Path", cfg.Services["api"].Path, filepath.Join(home, "dev", "example", "payments")},
		{"Container.Volumes[0]", cfg.Containers["db"].Volumes[0], filepath.Join(home, "db-data") + ":/var/lib/postgresql"},
		{"Container.Seed.Files[0]", cfg.Containers["db"].Seed.Files[0], filepath.Join(home, "seeds", "001-users.sql")},
		{"Sidecar.Volumes[0]", cfg.Containers["db"].Sidecars[0].Volumes[0], filepath.Join(home, "pgadmin-data") + ":/var/lib/pgadmin"},
		{"Container.Init.TopicsFile", cfg.Containers["kafka"].Init.TopicsFile, filepath.Join(home, "topics.yaml")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

// TestLoad_HomeExpansionRunsBeforeRelativeResolution locks the ordering
// invariant in Load: expandHomePaths must run before resolveRelativePaths,
// otherwise a "~/foo.yaml" topics_file is first joined to baseDir as
// "baseDir/~/foo.yaml" and the literal "~" survives into production.
func TestLoad_HomeExpansionRunsBeforeRelativeResolution(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	path := writeOrbitYAML(t, `
version: "3"
containers:
  kafka:
    image: confluentinc/cp-kafka:7.9.0
    init:
      type: kafka_topics
      topics_file: ~/topics.yaml
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(home, "topics.yaml")
	if got := cfg.Containers["kafka"].Init.TopicsFile; got != want {
		t.Errorf("topics_file = %q, want %q (must expand ~ before relative-path resolution joins it under baseDir)", got, want)
	}
}

func TestEnvVarDefaultValue(t *testing.T) {
	t.Setenv("NONEXISTENT_VAR", "")
	// t.Setenv restores on cleanup; we need it unset
	_ = os.Unsetenv("NONEXISTENT_VAR")

	result := substituteEnvVars("${NONEXISTENT_VAR:-/default/path}")
	if result != "/default/path" {
		t.Errorf("default not applied, got %q", result)
	}
}

func TestEnvVarNestedDefault_BothUnset_UsesInnermostDefault(t *testing.T) {
	t.Setenv("ORBIT_TEST_OUTER", "")
	_ = os.Unsetenv("ORBIT_TEST_OUTER")
	t.Setenv("ORBIT_TEST_INNER", "")
	_ = os.Unsetenv("ORBIT_TEST_INNER")

	got := substituteEnvVars("${ORBIT_TEST_OUTER:-${ORBIT_TEST_INNER:-/fallback}}")
	if got != "/fallback" {
		t.Errorf("nested default not resolved, got %q want %q", got, "/fallback")
	}
}

func TestEnvVarNestedDefault_OuterSet_PrefersOuter(t *testing.T) {
	t.Setenv("ORBIT_TEST_OUTER", "/from-outer")
	t.Setenv("ORBIT_TEST_INNER", "/from-inner")

	got := substituteEnvVars("${ORBIT_TEST_OUTER:-${ORBIT_TEST_INNER:-/fallback}}")
	if got != "/from-outer" {
		t.Errorf("got %q want %q", got, "/from-outer")
	}
}

func TestEnvVarNestedDefault_OuterUnsetInnerSet_UsesInner(t *testing.T) {
	t.Setenv("ORBIT_TEST_OUTER", "")
	_ = os.Unsetenv("ORBIT_TEST_OUTER")
	t.Setenv("ORBIT_TEST_INNER", "/from-inner")

	got := substituteEnvVars("${ORBIT_TEST_OUTER:-${ORBIT_TEST_INNER:-/fallback}}")
	if got != "/from-inner" {
		t.Errorf("got %q want %q", got, "/from-inner")
	}
}

func TestEnvVarNestedDefault_ThreeLevels(t *testing.T) {
	t.Setenv("ORBIT_TEST_A", "")
	_ = os.Unsetenv("ORBIT_TEST_A")
	t.Setenv("ORBIT_TEST_B", "")
	_ = os.Unsetenv("ORBIT_TEST_B")
	t.Setenv("ORBIT_TEST_C", "")
	_ = os.Unsetenv("ORBIT_TEST_C")

	got := substituteEnvVars("${ORBIT_TEST_A:-${ORBIT_TEST_B:-${ORBIT_TEST_C:-/deep}}}")
	if got != "/deep" {
		t.Errorf("got %q want %q", got, "/deep")
	}
}

func TestEnvVarUnclosedExpressionLeftIntact(t *testing.T) {
	got := substituteEnvVars("prefix-${UNCLOSED")
	if got != "prefix-${UNCLOSED" {
		t.Errorf("unclosed expression should be left intact, got %q", got)
	}
}

// Nested fallback composed with a literal suffix is the real-world shape
// the feature was added for: a yaml service path like
// "${PAYMENTS_ROOT:-${WORKSPACE_ROOT:-~/dev/example}}/payments" must resolve
// even when both vars are unset, and home expansion must still kick in.
func TestLoad_NestedEnvVarFallbackWithSuffix(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	t.Setenv("ORBIT_TEST_OUTER", "")
	_ = os.Unsetenv("ORBIT_TEST_OUTER")
	t.Setenv("ORBIT_TEST_INNER", "")
	_ = os.Unsetenv("ORBIT_TEST_INNER")

	path := writeOrbitYAML(t, `
version: "3"
services:
  payments:
    type: node
    path: ${ORBIT_TEST_OUTER:-${ORBIT_TEST_INNER:-~/dev/example}}/payments
    command: pnpm dev
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(home, "dev", "example", "payments")
	if got := cfg.Services["payments"].Path; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestCycleDetection(t *testing.T) {
	content := `
version: "3"
services:
  a:
    type: node
    path: /tmp/a
    command: echo a
    depends_on: [b]
  b:
    type: node
    path: /tmp/b
    command: echo b
    depends_on: [c]
  c:
    type: node
    path: /tmp/c
    command: echo c
    depends_on: [a]
`
	path := writeOrbitYAML(t, content)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestCycleDetection_SelfLoop(t *testing.T) {
	content := `
version: "3"
services:
  a:
    type: node
    path: /tmp/a
    command: echo a
    depends_on: [a]
`
	path := writeOrbitYAML(t, content)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected cycle detection error for self-loop, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestCycleDetection_DiamondNoCycle(t *testing.T) {
	content := `
version: "3"
services:
  a:
    type: node
    path: /tmp/a
    command: echo a
    depends_on: [b, c]
  b:
    type: node
    path: /tmp/b
    command: echo b
    depends_on: [d]
  c:
    type: node
    path: /tmp/c
    command: echo c
    depends_on: [d]
  d:
    type: node
    path: /tmp/d
    command: echo d
`
	path := writeOrbitYAML(t, content)

	if _, err := Load(path); err != nil {
		t.Fatalf("diamond DAG should not error, got: %v", err)
	}
}

func TestCycleDetection_Disconnected(t *testing.T) {
	content := `
version: "3"
services:
  a:
    type: node
    path: /tmp/a
    command: echo a
    depends_on: [b]
  b:
    type: node
    path: /tmp/b
    command: echo b
    depends_on: [c]
  c:
    type: node
    path: /tmp/c
    command: echo c
  x:
    type: node
    path: /tmp/x
    command: echo x
    depends_on: [y]
  y:
    type: node
    path: /tmp/y
    command: echo y
    depends_on: [x]
`
	path := writeOrbitYAML(t, content)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected cycle detection error for disconnected cluster, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestCycleDetection_MixedServiceContainer(t *testing.T) {
	content := `
version: "3"
containers:
  db:
    image: postgres:16
    ports:
      pg: 5432
    depends_on: [api]
services:
  api:
    type: node
    path: /tmp/api
    command: echo api
    depends_on: [db]
`
	path := writeOrbitYAML(t, content)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected cycle detection error for service↔container cycle, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestCycleDetection_LongChain(t *testing.T) {
	content := `
version: "3"
services:
  a:
    type: node
    path: /tmp/a
    command: echo a
    depends_on: [b]
  b:
    type: node
    path: /tmp/b
    command: echo b
    depends_on: [c]
  c:
    type: node
    path: /tmp/c
    command: echo c
    depends_on: [d]
  d:
    type: node
    path: /tmp/d
    command: echo d
    depends_on: [e]
  e:
    type: node
    path: /tmp/e
    command: echo e
    depends_on: [f]
  f:
    type: node
    path: /tmp/f
    command: echo f
`
	path := writeOrbitYAML(t, content)

	if _, err := Load(path); err != nil {
		t.Fatalf("6-node linear chain should not error, got: %v", err)
	}
}

func TestPortConflictDetection(t *testing.T) {
	content := `
version: "3"
containers:
  redis:
    image: redis:7.4
    ports:
      redis: 6379
services:
  api:
    type: node
    path: /tmp/api
    command: echo api
    ports:
      http: 6379
`
	path := writeOrbitYAML(t, content)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected port conflict error, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestDualDefinition_AllowsSameNameInBoth(t *testing.T) {
	content := `
version: "3"
containers:
  redis:
    image: redis:7.4
    ports:
      redis: 6379
services:
  redis:
    type: node
    path: /tmp/redis
    command: echo redis
    ports:
      http: 8080
`
	path := writeOrbitYAML(t, content)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("dual-definition should be allowed, got: %v", err)
	}
	if _, ok := cfg.Containers["redis"]; !ok {
		t.Error("redis container not found")
	}
	if _, ok := cfg.Services["redis"]; !ok {
		t.Error("redis service not found")
	}
}

func TestInvalidPullPolicyDetection(t *testing.T) {
	content := `
version: "3"
containers:
  db:
    image: local/db:dev
    pull_policy: sometimes
    ports:
      mssql: 1433
`
	path := writeOrbitYAML(t, content)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected invalid pull policy error, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestSidecarPullPolicy(t *testing.T) {
	content := `
version: "3"
containers:
  kafka:
    image: kafka:dev
    ports:
      broker: 19092
    sidecars:
      - name: producer
        image: orbit-kafka-producer:local
        pull_policy: never
        ports:
          api: 18081
`
	path := writeOrbitYAML(t, content)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.Containers["kafka"].Sidecars[0].PullPolicy; got != "never" {
		t.Fatalf("sidecar pull policy = %q, want never", got)
	}
}

func TestInvalidSidecarPullPolicyDetection(t *testing.T) {
	content := `
version: "3"
containers:
  kafka:
    image: kafka:dev
    ports:
      broker: 19092
    sidecars:
      - name: producer
        image: orbit-kafka-producer:local
        pull_policy: sometimes
`
	path := writeOrbitYAML(t, content)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected invalid sidecar pull policy error, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestLoad_ResolvesRelativeTopicsFile(t *testing.T) {
	tmp, err := os.MkdirTemp("", "orb-cfg-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	cfgPath := filepath.Join(tmp, "development.yaml")
	dataPath := filepath.Join(tmp, "data", "kafka-topics.yaml")
	if err := os.MkdirAll(filepath.Dir(dataPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte("topics: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	yaml := `version: "3"
containers:
  kafka:
    image: kafka:latest
    init:
      type: kafka_topics
      topics_file: ./data/kafka-topics.yaml
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.Containers["kafka"].Init.TopicsFile
	if !filepath.IsAbs(got) {
		t.Errorf("topics_file = %q, want absolute path", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("topics_file %q does not resolve to an existing file: %v", got, err)
	}
}

func TestLoad_UnknownField(t *testing.T) {
	yamlContent := `version: "3"
containers:
  sql-server:
    image: foo:latest
    imagee: typo
`
	dir := t.TempDir()
	path := filepath.Join(dir, "env.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), `imagee`) ||
		!strings.Contains(err.Error(), `did you mean "image"`) {
		t.Errorf("error %q should identify and correct the unknown field", err.Error())
	}
}

func TestLoad_SchemaErrorsSpeakConfigVocabulary(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "type mismatch under a named section",
			content: "version: \"3\"\nservices:\n  web:\n  kind: frontend\n",
			want:    "a services entry",
		},
		{
			name:    "unknown field under a named section",
			content: "version: \"3\"\nservices:\n  web:\n    kind: frontend\n    portz: {}\n",
			want:    "unknown field portz in a services entry",
		},
		{
			name:    "unknown top-level section",
			content: "version: \"3\"\nservicez:\n  web:\n    kind: frontend\n",
			want:    "unknown top-level section servicez",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "env.yaml")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatal("expected a schema error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "config.") {
				t.Errorf("error leaks a Go type name: %q", err)
			}
		})
	}
}

func TestLoad_RealEnvFiles(t *testing.T) {
	files := []string{
		"../envs/example.yaml",
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			if _, err := Load(f); err != nil {
				t.Errorf("Load(%s) failed: %v", f, err)
			}
		})
	}
}

func TestLoad_VersionMismatch(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantContain string
	}{
		{
			name:        "env newer than binary",
			yaml:        `version: "4"` + "\n",
			wantContain: "Orbit binary is out of date",
		},
		{
			name:        "env older than binary",
			yaml:        `version: "2"` + "\n",
			wantContain: schemaMigrationGuideURL,
		},
		{
			name:        "missing version",
			yaml:        `settings:` + "\n  shutdown_timeout: 30s\n",
			wantContain: "missing required field 'version'",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "env.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantContain) {
				t.Errorf("error %q missing substring %q", err.Error(), tc.wantContain)
			}
		})
	}
}

func TestLoadChecksVersionBeforeLegacyFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	source := `version: "2"
containers:
  database:
    image: database:latest
    seed:
      type: sqlserver
      database: app
      files: [seed.sql]
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	var mismatch *SchemaVersionMismatchError
	if !errors.As(err, &mismatch) || mismatch.Kind != SchemaVersionOlder {
		t.Fatalf("error = %v, want older schema guidance before legacy-field parsing", err)
	}
}

func TestLoad_PopulatesExternalName(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "env.yaml")
	yamlSrc := `version: "3"
externals:
  upstream:
    label: Upstream
    kafka:
      produces: [upstream.pricing.odds]
`
	if err := os.WriteFile(path, []byte(yamlSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ext, ok := cfg.Externals["upstream"]
	if !ok {
		t.Fatal("externals[upstream] missing")
	}
	if ext.Name != "upstream" {
		t.Errorf("Name = %q, want upstream", ext.Name)
	}
}

func TestLoadAcceptsReadablePreferredPortMappings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferred-ports.yaml")
	source := `version: "3"
containers:
  redis:
    image: redis:7.4-alpine
    ports:
      redis:
        preferred: 26379
        target: 6379
services:
  api:
    type: python
    path: .
    command: python3 app.py
    ports:
      http:
        preferred: 28080
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	redis := cfg.Containers["redis"].Ports["redis"]
	if redis.Host != 26379 || redis.Target != 6379 {
		t.Fatalf("redis port = %+v, want fixed 26379:6379", redis)
	}
	http := cfg.Services["api"].Ports["http"]
	if http.Host != 28080 || http.Target != 28080 {
		t.Fatalf("http port = %+v, want fixed 28080", http)
	}
}

func TestLoadRejectsUnknownPreferredPortFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid-preferred-port.yaml")
	source := `version: "3"
services:
  api:
    type: python
    path: .
    command: python3 app.py
    ports:
      http:
        preferred: 28080
        typo: 28081
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("unknown preferred port field was accepted")
	} else if !strings.Contains(err.Error(), `line 10: unknown port field "typo"`) {
		t.Fatalf("error = %q", err)
	}
}

func TestLoadRejectsPortMappingWithoutPreferred(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target-only-port.yaml")
	source := `version: "3"
services:
  api:
    type: python
    path: .
    command: python3 app.py
    ports:
      http:
        target: 8080
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("port mapping without preferred was accepted")
	}
	if !strings.Contains(err.Error(), `needs "preferred"`) {
		t.Errorf("error = %q, want it to name the missing field", err)
	}
	if strings.Contains(err.Error(), "preferred port 0") {
		t.Errorf("error blames a field the author never wrote: %q", err)
	}
}

func TestLoadTreatsConfigAsAnAuthoringContract(t *testing.T) {
	tests := []struct {
		name    string
		service string
		want    string
	}{
		{
			name: "invalid URL",
			service: `    command: python3 app.py
    url: not-a-url`,
			want: `service "app" url must be an absolute http or https URL`,
		},
		{
			name: "URL disagrees with declared endpoint",
			service: `    command: python3 app.py
    url: http://localhost:28412
    ports:
      http: 28411`,
			want: `service "app" url uses port 28412 but ports.http declares 28411`,
		},
		{
			name: "preferred port typo",
			service: `    command: python3 app.py
    ports:
      http:
        prefered: 28411`,
			want: `unknown port field "prefered" (did you mean "preferred"?)`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "orbit.yaml")
			source := "version: \"3\"\nservices:\n  app:\n" + test.service + "\n"
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("invalid config was accepted")
			} else if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want %q", err, test.want)
			}
		})
	}
}

func TestLoadDefaultsServicePathAndInfersRuntime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orbit.yaml")
	if err := os.WriteFile(path, []byte(`version: "3"
services:
  app:
    command: python3 app.py
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	service := cfg.Services["app"]
	if service.Path != dir {
		t.Fatalf("path = %q, want %q", service.Path, dir)
	}
	if service.Type != "python" {
		t.Fatalf("type = %q, want python", service.Type)
	}
}

func TestValidateRejectsOverlappingHostPorts(t *testing.T) {
	cfg := &Config{
		Containers: map[string]*Container{
			"one":   {Ports: map[string]PortDef{"http": {Host: 28080, Target: 28080}}},
			"other": {Ports: map[string]PortDef{"http": {Host: 28080, Target: 8080}}},
		},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("two resources declaring the same host port must be a config error")
	}
}
