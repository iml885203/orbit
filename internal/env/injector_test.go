package env

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/port"
)

func p(port int) config.PortDef {
	return config.PortDef{Host: port, Target: port}
}

func configWithContainers(containers map[string]*config.Container) *config.Config {
	return &config.Config{
		Containers: containers,
		Services:   map[string]*config.Service{},
	}
}

func TestBuildEnv_Redis(t *testing.T) {
	svc := &config.Service{
		Name:      "api",
		DependsOn: []string{"redis"},
	}
	containers := map[string]*config.Container{
		"redis": {
			Name:  "redis",
			Image: "redis:7.4",
			Ports: map[string]config.PortDef{"redis": p(6379)},
		},
	}

	env := BuildEnv(svc, configWithContainers(containers), nil)

	if env["REDIS_URL"] != "localhost:6379" {
		t.Errorf("REDIS_URL = %q, want localhost:6379", env["REDIS_URL"])
	}
}

func TestBuildEnv_Kafka(t *testing.T) {
	svc := &config.Service{
		Name:      "api",
		DependsOn: []string{"kafka"},
	}
	containers := map[string]*config.Container{
		"kafka": {
			Name:  "kafka",
			Image: "confluentinc/cp-kafka:7.9.0",
			Ports: map[string]config.PortDef{"broker": p(9092)},
		},
	}

	env := BuildEnv(svc, configWithContainers(containers), nil)

	if env["KAFKA_BOOTSTRAP_SERVERS"] != "localhost:9092" {
		t.Errorf("KAFKA_BOOTSTRAP_SERVERS = %q", env["KAFKA_BOOTSTRAP_SERVERS"])
	}
}

func TestBuildEnv_ExplicitOverride(t *testing.T) {
	svc := &config.Service{
		Name:      "api",
		DependsOn: []string{"redis"},
		Env:       map[string]string{"REDIS_URL": "custom:1234"},
	}
	containers := map[string]*config.Container{
		"redis": {
			Name:  "redis",
			Image: "redis:7.4",
			Ports: map[string]config.PortDef{"redis": p(6379)},
		},
	}

	env := BuildEnv(svc, configWithContainers(containers), nil)

	if env["REDIS_URL"] != "custom:1234" {
		t.Errorf("explicit env should override auto, got %q", env["REDIS_URL"])
	}
}

func TestBuildEnv_DoesNotInferSQLServerCredentials(t *testing.T) {
	svc := &config.Service{
		Name:      "worker",
		DependsOn: []string{"sql-server"},
	}
	containers := map[string]*config.Container{
		"sql-server": {
			Name:        "sql-server",
			Image:       "mcr.microsoft.com/mssql/server:2022-latest",
			Ports:       map[string]config.PortDef{"mssql": p(1433)},
			Environment: map[string]string{"SA_PASSWORD": "test123"},
		},
	}

	env := BuildEnv(svc, configWithContainers(containers), nil)

	if _, ok := env["SQL_SERVER_CONNECTION"]; ok {
		t.Fatal("image detection leaked SQL Server credentials into a dependent service")
	}
	if env["SQL_SERVER_MSSQL_PORT"] != "1433" {
		t.Errorf("generic declared port missing: %+v", env)
	}
}

func TestBuildEnv_ConnectionStrings(t *testing.T) {
	svc := &config.Service{
		Name:      "api",
		DependsOn: []string{"redis", "mongodb"},
	}
	containers := map[string]*config.Container{
		"redis": {
			Name:  "redis",
			Image: "redis:7.4",
			Ports: map[string]config.PortDef{"redis": p(16379)},
		},
		"mongodb": {
			Name:  "mongodb",
			Image: "mongo:6.0.8",
			Ports: map[string]config.PortDef{"mongo": p(27018)},
		},
	}

	env := BuildEnv(svc, configWithContainers(containers), nil)

	if env["ConnectionStrings__redis"] != "localhost:16379" {
		t.Errorf("ConnectionStrings__redis = %q", env["ConnectionStrings__redis"])
	}
	if env["ConnectionStrings__mongodb"] != "mongodb://localhost:27018" {
		t.Errorf("ConnectionStrings__mongodb = %q", env["ConnectionStrings__mongodb"])
	}
}

func TestInjectServicePortsAddsConventionalNamesWithoutOverridingExplicitEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.yaml")
	source := `version: "3"
services:
  api:
    type: python
    path: .
    command: python3 app.py
    ports:
      http: "${ORBIT_AUTO_PORT_ENV_TEST_HTTP:-28080}"
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	httpPort := cfg.Services["api"].Ports["http"]
	httpPort.Host = 28081
	cfg.Services["api"].Ports["http"] = httpPort

	resolved := map[string]string{"HTTP_PORT": "explicit", "PORT": "28080"}
	InjectServicePorts(resolved, cfg.Services["api"].Ports)
	if resolved["PORT"] != "28081" {
		t.Fatalf("PORT = %q, want 28081", resolved["PORT"])
	}
	if resolved["HTTP_PORT"] != "explicit" {
		t.Fatalf("HTTP_PORT override = %q", resolved["HTTP_PORT"])
	}
}

func TestBuildEnvInjectsRuntimeServiceURLWithoutOverridingExplicitEnv(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"catalog-api": {
				Name: "catalog-api",
				URL:  "http://localhost:3011",
			},
		},
		Containers: map[string]*config.Container{},
	}
	consumer := &config.Service{
		Name:      "checkout-api",
		DependsOn: []string{"catalog-api"},
	}

	resolved := BuildEnv(consumer, cfg, nil)
	if resolved["CATALOG_API_URL"] != "http://localhost:3011" {
		t.Fatalf("CATALOG_API_URL = %q", resolved["CATALOG_API_URL"])
	}

	consumer.Env = map[string]string{"CATALOG_API_URL": "https://catalog.example.test"}
	resolved = BuildEnv(consumer, cfg, nil)
	if resolved["CATALOG_API_URL"] != "https://catalog.example.test" {
		t.Fatalf("explicit CATALOG_API_URL = %q", resolved["CATALOG_API_URL"])
	}
	annotated := AnnotatedEnv(consumer, cfg, nil)
	if len(annotated) != 1 ||
		annotated[0].Key != "CATALOG_API_URL" ||
		annotated[0].Source != "explicit" ||
		annotated[0].Dependency != "" {
		t.Fatalf("annotated explicit override = %+v", annotated)
	}
}

func TestBuildEnvInjectsAutoSelectedDependencyURL(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	preferred := listener.Addr().(*net.TCPAddr).Port
	path := filepath.Join(t.TempDir(), "env.yaml")
	source := fmt.Sprintf(`version: "3"
services:
  catalog-api:
    type: python
    path: .
    command: python3 app.py
    ports:
      http: "${ORBIT_AUTO_PORT_ENV_UPSTREAM_TEST:-%d}"
  checkout-api:
    type: python
    path: .
    command: python3 app.py
    depends_on: [catalog-api]
`, preferred)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := port.ResolveAutoPorts(cfg, nil); err != nil {
		t.Fatal(err)
	}

	actual := cfg.Services["catalog-api"].Ports["http"].Host
	if actual == preferred {
		t.Fatalf("occupied preference %d was not moved", preferred)
	}
	resolved := BuildEnv(cfg.Services["checkout-api"], cfg, nil)
	want := fmt.Sprintf("http://localhost:%d", actual)
	if resolved["CATALOG_API_URL"] != want {
		t.Fatalf("CATALOG_API_URL = %q, want %q", resolved["CATALOG_API_URL"], want)
	}
}

func TestEnvVarsForDependency(t *testing.T) {
	containers := map[string]*config.Container{
		"redis":      {Name: "redis", Image: "redis:7.4", Ports: map[string]config.PortDef{"redis": {Host: 16379, Target: 6379}}},
		"sql-server": {Name: "sql-server", Image: "mcr.microsoft.com/mssql/server:2022-latest", Ports: map[string]config.PortDef{"mssql": {Host: 11433, Target: 1433}}, Environment: map[string]string{"SA_PASSWORD": "test123"}},
	}
	cfg := configWithContainers(containers)

	got := EnvVarsForDependency("redis", cfg)
	wantSubset := []string{"REDIS_URL", "ConnectionStrings__redis", "REDIS_HOST", "REDIS_PORT"}
	for _, k := range wantSubset {
		if !contains(got, k) {
			t.Errorf("EnvVarsForDependency(redis): missing %q in %v", k, got)
		}
	}

	got = EnvVarsForDependency("sql-server", cfg)
	if contains(got, "SQL_SERVER_CONNECTION") || contains(got, "ConnectionStrings__sql-server") {
		t.Errorf("EnvVarsForDependency(sql-server) inferred credential-bearing keys: %v", got)
	}
}

func TestEnvVarsForDependency_UnknownReturnsEmpty(t *testing.T) {
	got := EnvVarsForDependency("nope", &config.Config{})
	if len(got) != 0 {
		t.Errorf("EnvVarsForDependency(nil) = %v, want empty", got)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
