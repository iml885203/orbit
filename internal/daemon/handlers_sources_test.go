package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/envsource"
)

func TestSourceAPIAddsLocalSourceAndPublishesNestedEnvironments(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(local, "envs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "envs", "e2e.yaml"), []byte("version: \"3\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(sourceMutationRequest{Action: "add", Name: "env-dev", Type: envsource.TypeLocal, Path: local})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/sources", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	srv.handleSources(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("add status = %d, body = %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/envs", nil)
	response = httptest.NewRecorder()
	srv.handleEnvs(response, request)
	var environments EnvsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &environments); err != nil {
		t.Fatal(err)
	}
	if len(environments.Sources) != 1 || environments.Sources[0].Name != "env-dev" ||
		len(environments.Sources[0].Environments) != 1 || environments.Sources[0].Environments[0].Identity != "env-dev/e2e" {
		t.Fatalf("environment sources = %+v", environments.Sources)
	}
}

func TestSourceAPIPreservesLocalDirectoryOnRemoval(t *testing.T) {
	srv := newTestServer(t, &config.Config{})
	local := t.TempDir()
	envs := filepath.Join(local, "envs")
	if err := os.MkdirAll(envs, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(envs, "e2e.yaml")
	if err := os.WriteFile(file, []byte("version: \"3\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	registry, err := envsource.Load(envsource.RegistryPath(OrbitDir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "env-dev", Type: envsource.TypeLocal, Path: local}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/sources", strings.NewReader(`{"action":"remove","name":"env-dev","confirmed":true}`))
	response := httptest.NewRecorder()
	srv.handleSources(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("remove status = %d, body = %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("local source directory changed: %v", err)
	}
}
