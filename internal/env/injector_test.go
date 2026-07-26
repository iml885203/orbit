package env

import (
	"testing"

	"github.com/iml885203/orbit/config"
)

func p(port int) config.PortDef {
	return config.PortDef{Host: port, Target: port}
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

	env := BuildEnv(svc, containers, nil)

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

	env := BuildEnv(svc, containers, nil)

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

	env := BuildEnv(svc, containers, nil)

	if env["REDIS_URL"] != "custom:1234" {
		t.Errorf("explicit env should override auto, got %q", env["REDIS_URL"])
	}
}

func TestBuildEnv_SqlServer(t *testing.T) {
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

	env := BuildEnv(svc, containers, nil)

	want := "Server=localhost,1433;User Id=sa;Password=test123;TrustServerCertificate=true"
	if env["SQL_SERVER_CONNECTION"] != want {
		t.Errorf("SQL_SERVER_CONNECTION = %q, want %q", env["SQL_SERVER_CONNECTION"], want)
	}
	if env["ConnectionStrings__sql-server"] != want {
		t.Errorf("ConnectionStrings__sql-server = %q, want %q", env["ConnectionStrings__sql-server"], want)
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

	env := BuildEnv(svc, containers, nil)

	if env["ConnectionStrings__redis"] != "localhost:16379" {
		t.Errorf("ConnectionStrings__redis = %q", env["ConnectionStrings__redis"])
	}
	if env["ConnectionStrings__mongodb"] != "mongodb://localhost:27018" {
		t.Errorf("ConnectionStrings__mongodb = %q", env["ConnectionStrings__mongodb"])
	}
}

func TestEnvVarsForDependency(t *testing.T) {
	containers := map[string]*config.Container{
		"redis":      {Name: "redis", Image: "redis:7.4", Ports: map[string]config.PortDef{"redis": {Host: 16379, Target: 6379}}},
		"sql-server": {Name: "sql-server", Image: "mcr.microsoft.com/mssql/server:2022-latest", Ports: map[string]config.PortDef{"mssql": {Host: 11433, Target: 1433}}, Environment: map[string]string{"SA_PASSWORD": "test123"}},
	}

	got := EnvVarsForDependency("redis", containers["redis"])
	wantSubset := []string{"REDIS_URL", "ConnectionStrings__redis", "REDIS_HOST", "REDIS_PORT"}
	for _, k := range wantSubset {
		if !contains(got, k) {
			t.Errorf("EnvVarsForDependency(redis): missing %q in %v", k, got)
		}
	}

	got = EnvVarsForDependency("sql-server", containers["sql-server"])
	if !contains(got, "SQL_SERVER_CONNECTION") || !contains(got, "ConnectionStrings__sql-server") {
		t.Errorf("EnvVarsForDependency(sql-server): missing expected keys in %v", got)
	}
}

func TestEnvVarsForDependency_UnknownReturnsEmpty(t *testing.T) {
	got := EnvVarsForDependency("nope", nil)
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
