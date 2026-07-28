package app

import (
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

func TestFindPostgresContainerUsesPortLabelOrClearName(t *testing.T) {
	byPort := &config.Config{Containers: map[string]*config.Container{
		"database": {Ports: map[string]config.PortDef{"postgres": {Host: 5432, Target: 5432}}},
	}}
	if got := findPostgresContainer(byPort); got != "database" {
		t.Fatalf("port-labelled container = %q, want database", got)
	}

	byName := &config.Config{Containers: map[string]*config.Container{
		"postgresql": {},
	}}
	if got := findPostgresContainer(byName); got != "postgresql" {
		t.Fatalf("named container = %q, want postgresql", got)
	}
}
