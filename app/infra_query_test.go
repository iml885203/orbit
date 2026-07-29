package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestQueryCommandIncludesPostgresWithExplicitDatabaseOverride(t *testing.T) {
	query := queryCmd()
	postgres, _, err := query.Find([]string{"postgres"})
	if err != nil {
		t.Fatal(err)
	}
	if postgres.Flags().Lookup("database") == nil {
		t.Fatal("postgres query command has no --database flag")
	}
	if !slices.Contains(postgres.Aliases, "postgresql") {
		t.Fatalf("postgres aliases = %v, want postgresql", postgres.Aliases)
	}
}

func TestPostgresQueryDockerArgsKeepsSQLOutOfTheShellProgram(t *testing.T) {
	query := `SELECT * FROM users WHERE name = 'Ada';`
	got := postgresQueryDockerArgs("orbit-postgres", "app", []string{query})

	if got[0] != "exec" || got[2] != "orbit-postgres" {
		t.Fatalf("docker target args = %v", got)
	}
	if got[len(got)-2] != "app" || got[len(got)-1] != query {
		t.Fatalf("database/query positional args = %v", got)
	}
	if strings.Contains(got[5], query) {
		t.Fatal("query was interpolated into the shell program")
	}
}

func TestQueryContainerSelectionIsPredictable(t *testing.T) {
	cfg := &config.Config{Containers: map[string]*config.Container{
		"app-db": {
			Ports: map[string]config.PortDef{"postgres": {Host: 5432, Target: 5432}},
		},
		"analytics-db": {
			Ports: map[string]config.PortDef{"postgres": {Host: 5433, Target: 5432}},
		},
		"cache": {
			Ports: map[string]config.PortDef{"redis": {Host: 6379, Target: 6379}},
		},
	}}

	if got, err := resolveQueryContainer(cfg, "PostgreSQL", "analytics-db", "postgres", "postgresql"); err != nil ||
		got != "analytics-db" {
		t.Fatalf("explicit PostgreSQL selection = %q, %v", got, err)
	}
	if got, err := resolveQueryContainer(cfg, "Redis", "", "redis"); err != nil || got != "cache" {
		t.Fatalf("single Redis selection = %q, %v", got, err)
	}
	if _, err := resolveQueryContainer(cfg, "MongoDB", "missing", "mongo"); err == nil ||
		!strings.Contains(err.Error(), "analytics-db, app-db, cache") {
		t.Fatalf("missing explicit target = %v", err)
	}
}

func TestQueryCommandRejectsAnAmbiguousTargetBeforeDocker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	source := `version: "3"
containers:
  app-db:
    image: postgres:18
    ports:
      postgres: "5432:5432"
  analytics-db:
    image: postgres:18
    ports:
      postgres: "5433:5432"
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	previousConfigFile := configFile
	configFile = path
	t.Cleanup(func() { configFile = previousConfigFile })

	command := queryCmd()
	command.SetArgs([]string{"postgres", "SELECT 1"})
	command.SilenceUsage = true
	err := command.Execute()
	if err == nil ||
		!strings.Contains(err.Error(), "analytics-db, app-db") ||
		!strings.Contains(err.Error(), "--container") {
		t.Fatalf("ambiguous query error = %v", err)
	}
}
