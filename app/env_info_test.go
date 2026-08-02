package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

func loadEnvInfoFixture(t *testing.T) (*config.Config, string) {
	t.Helper()
	raw := `version: "3"
containers:
  db:
    image: postgres:16
    ports:
      pg: "15432:5432"
    environment:
      POSTGRES_PASSWORD: hunter2
      POSTGRES_USER: dev
services:
  api:
    command: python3 -m http.server 0
    url: http://localhost:18080/docs
    ports:
      http:
        preferred: 18080
    env:
      API_KEY: secret
`
	path := filepath.Join(t.TempDir(), "info.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return cfg, path
}

func TestBuildEnvInfoReportsDeclaredValuesWithoutADaemon(t *testing.T) {
	cfg, path := loadEnvInfoFixture(t)

	data := buildEnvInfoJSONData(cfg, path, envInfoDaemon{}, nil, false)

	if data.Env.Name != "info" || data.Env.ConfigPath == "" {
		t.Fatalf("env identity = %+v", data.Env)
	}
	db := data.Containers["db"]
	if db.State != "" {
		t.Fatalf("db state = %q, want none without a daemon", db.State)
	}
	if got := db.Ports["pg"]; got.Declared != 15432 || got.Target != 5432 || got.Observed != 0 {
		t.Fatalf("db pg port = %+v", got)
	}
	if !reflect.DeepEqual(db.EnvironmentKeys, []string{"POSTGRES_PASSWORD", "POSTGRES_USER"}) {
		t.Fatalf("db environment keys = %v", db.EnvironmentKeys)
	}
	if db.Environment != nil {
		t.Fatalf("db environment values leaked without --show-secrets: %v", db.Environment)
	}
	api := data.Services["api"]
	if got := api.Ports["http"]; got.Declared != 18080 || got.Observed != 0 {
		t.Fatalf("api http port = %+v", got)
	}
	if api.URL == nil || api.URL.Declared != "http://localhost:18080/docs" || api.URL.Observed != "" {
		t.Fatalf("api url = %+v", api.URL)
	}
}

func TestBuildEnvInfoAttachesObservedValuesFromStatus(t *testing.T) {
	cfg, path := loadEnvInfoFixture(t)
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "db", State: "healthy", Ports: map[string]int{"pg": 15432}},
		{Name: "api", State: "healthy", Ports: map[string]int{"http": 18081}, URL: "http://localhost:18081"},
		{Name: "not-in-config", State: "healthy"},
	}}

	data := buildEnvInfoJSONData(cfg, path, envInfoDaemon{Running: true, ConfigMatch: true}, status, false)

	api := data.Services["api"]
	if api.State != "healthy" {
		t.Fatalf("api state = %q", api.State)
	}
	if got := api.Ports["http"]; got.Declared != 18080 || got.Observed != 18081 {
		t.Fatalf("api http port = %+v, want declared preference and observed relocation both visible", got)
	}
	if api.URL == nil || api.URL.Observed != "http://localhost:18081" {
		t.Fatalf("api url = %+v", api.URL)
	}
	if _, ok := data.Containers["not-in-config"]; ok {
		t.Fatal("a resource unknown to the config leaked into the payload")
	}
	if _, ok := data.Services["not-in-config"]; ok {
		t.Fatal("a resource unknown to the config leaked into the payload")
	}
}

func TestBuildEnvInfoIncludesEnvironmentValuesOnlyOnRequest(t *testing.T) {
	cfg, path := loadEnvInfoFixture(t)

	data := buildEnvInfoJSONData(cfg, path, envInfoDaemon{}, nil, true)

	if got := data.Containers["db"].Environment["POSTGRES_PASSWORD"]; got != "hunter2" {
		t.Fatalf("db POSTGRES_PASSWORD = %q with --show-secrets", got)
	}
	if got := data.Services["api"].Environment["API_KEY"]; got != "secret" {
		t.Fatalf("api API_KEY = %q with --show-secrets", got)
	}
}

func TestEnvInfoActionsSuggestStartingOnlyWhenObservationIsMissing(t *testing.T) {
	if actions := envInfoActions(envInfoDaemon{Running: true, ConfigMatch: true}); actions != nil {
		t.Fatalf("actions = %+v, want none while observing", actions)
	}
	actions := envInfoActions(envInfoDaemon{})
	if len(actions) != 1 || actions[0].Command != "orbit up --json" {
		t.Fatalf("actions = %+v", actions)
	}
}
