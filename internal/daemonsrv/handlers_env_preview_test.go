package daemonsrv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/engine"
)

func TestEnvironmentPreviewMatchesApplyImpactWithoutPublishing(t *testing.T) {
	const original = `version: "3"
containers:
  db:
    image: postgres:17
  cache:
    image: redis:7
services:
  api:
    command: echo old
  web:
    command: echo web
    depends_on: [api]
  idle:
    command: echo idle
`
	for _, tt := range []struct {
		name                            string
		mutate                          func(*config.Config)
		running                         []string
		stop, restart, removed, started []string
		handoff                         bool
	}{
		{name: "no changes", running: []string{"api", "web", "db"}},
		{name: "changed resource and dependent", mutate: func(c *config.Config) { c.Services["api"].Command = "echo new" }, running: []string{"api", "web", "db"}, stop: []string{"api", "web"}, restart: []string{"api", "web"}},
		{name: "running resource removed", mutate: func(c *config.Config) { delete(c.Services, "idle") }, running: []string{"idle", "db"}, stop: []string{"idle"}, removed: []string{"idle"}},
		{name: "stopped resource removed", mutate: func(c *config.Config) { delete(c.Services, "idle") }, running: []string{"db"}, removed: []string{"idle"}},
		{name: "new stopped dependency", mutate: func(c *config.Config) { c.Services["api"].DependsOn = []string{"cache"} }, running: []string{"api", "web", "db"}, stop: []string{"api", "web"}, restart: []string{"api", "web"}, started: []string{"cache"}},
		{name: "new dependency resource", mutate: func(c *config.Config) {
			c.Services["worker"] = &config.Service{Name: "worker", Command: "echo worker"}
			c.Services["api"].DependsOn = []string{"worker"}
		}, running: []string{"api", "db"}, stop: []string{"api"}, restart: []string{"api"}, started: []string{"worker"}},
		{name: "daemon handoff", mutate: func(c *config.Config) { c.Settings.ShutdownTimeout++ }, running: []string{"api", "web", "db"}, stop: []string{"api", "db", "web"}, restart: []string{"api", "db", "web"}, handoff: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "orbit.yaml")
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			old, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			srv := newTestServer(t, old)
			srv.SetConfigPath(path)
			for _, name := range tt.running {
				srv.app.Orchestrator.MarkServiceHealthy(name)
			}
			next, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if tt.mutate != nil {
				tt.mutate(next)
				srv.engineStale.Store(true)
			}
			before := sortedPreviewServices(srv)
			holder := srv.holder.Load()
			generation := srv.environmentGeneration.Load()
			baseline := srv.baseline
			got, err := srv.previewEnvironmentConfig(next)
			if err != nil {
				t.Fatal(err)
			}
			for _, pair := range []struct {
				label     string
				got, want []string
			}{{"stop", got.StopResources, tt.stop}, {"restart", got.RestartResources, tt.restart}, {"removed", got.RemovedResources, tt.removed}, {"started", got.StartedDependencies, tt.started}} {
				if !reflect.DeepEqual(pair.got, append([]string{}, pair.want...)) {
					t.Fatalf("%s=%v want %v", pair.label, pair.got, pair.want)
				}
			}
			if got.RestartRequired != tt.handoff || got.ChangesPending != (tt.mutate != nil) || got.TargetFingerprint == "" || got.PID == 0 || got.ObservedAt.IsZero() {
				t.Fatalf("preview=%+v", got)
			}
			if srv.holder.Load() != holder || srv.environmentGeneration.Load() != generation || !reflect.DeepEqual(sortedPreviewServices(srv), before) || !reflect.DeepEqual(srv.baseline, baseline) {
				t.Fatal("preview changed published/runtime state")
			}
		})
	}
}

func TestEnvironmentPreviewReadOnlyEndpointAndInvalidContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	original := []byte("version: \"3\"\nservices:\n  api:\n    command: echo old\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, cfg)
	srv.SetConfigPath(path)
	srv.app.Orchestrator.MarkServiceHealthy("api")
	srv.engineStale.Store(true)
	before := sortedPreviewServices(srv)
	generation := srv.environmentGeneration.Load()
	baseline := srv.baseline
	holder := srv.holder.Load()
	for _, tt := range []struct {
		name, method, requested, content string
		status                           int
	}{
		{"valid", http.MethodGet, path, "version: \"3\"\nservices:\n  api:\n    command: echo new\n", 200},
		{"wrong context", http.MethodGet, path + ".wrong", string(original), 409},
		{"missing context", http.MethodGet, "", string(original), 409},
		{"invalid config", http.MethodGet, path, "version: \"3\"\nservices: [invalid]", 400},
		{"wrong method", http.MethodPost, path, string(original), 405},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			srv.handleEnvironmentPreview(recorder, httptest.NewRequest(tt.method, "/api/env/preview?config_path="+url.QueryEscape(tt.requested), nil))
			if recorder.Code != tt.status {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if tt.status == 200 {
				var got daemon.EnvironmentApplyPreview
				if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got.StopResources, []string{"api"}) {
					t.Fatalf("preview=%+v", got)
				}
			}
			if srv.holder.Load() != holder || srv.environmentGeneration.Load() != generation || !srv.engineStale.Load() || !reflect.DeepEqual(sortedPreviewServices(srv), before) || !reflect.DeepEqual(srv.baseline, baseline) {
				t.Fatal("preview changed environment state")
			}
		})
	}
}

func sortedPreviewServices(s *Server) []engine.ServiceInfo {
	values := s.app.Orchestrator.GetAllServices()
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return values
}

func TestEnvironmentPreviewUsesDetachedEdgesAndRuntimeMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	original := []byte("version: \"3\"\ncontainers:\n  cache:\n    image: redis:7\n  api:\n    image: api:1\nservices:\n  api:\n    command: echo old\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	old, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, old)
	srv.SetConfigPath(path)
	srv.app.Orchestrator.MarkServiceHealthy("api")
	if err := srv.settings.SetServiceMode("api", "container"); err != nil {
		t.Fatal(err)
	}
	next, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv.engineStale.Store(true)
	next.Services["api"].Command = "echo inactive mode change"
	got, err := srv.previewEnvironmentConfig(next)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.StopResources) != 0 {
		t.Fatalf("inactive-mode edit interrupted container: %+v", got)
	}
	if err := srv.settings.SetServiceMode("api", "service"); err != nil {
		t.Fatal(err)
	}
	if err := srv.settings.SetEdgeDetached("orbit", "api", "cache", true); err != nil {
		t.Fatal(err)
	}
	settingsBefore, err := srv.settings.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	next.Services["api"].DependsOn = []string{"cache"}
	got, err = srv.previewEnvironmentConfig(next)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.StartedDependencies) != 0 || !reflect.DeepEqual(got.RestartResources, []string{"api"}) {
		t.Fatalf("detached dependency included: %+v", got)
	}
	settingsAfter, err := srv.settings.Snapshot()
	if err != nil || !reflect.DeepEqual(settingsBefore, settingsAfter) {
		t.Fatal("preview changed persisted runtime settings")
	}
}

func TestEnvironmentPreviewRetainsApplyAdmissionForEnvironmentOnlyChanges(t *testing.T) {
	t.Setenv("PREVIEW_COMMAND", "echo old")
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	original := []byte("version: \"3\"\nservices:\n  api:\n    command: ${PREVIEW_COMMAND}\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	old, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, old)
	srv.SetConfigPath(path)
	srv.app.Orchestrator.MarkServiceHealthy("api")
	t.Setenv("PREVIEW_COMMAND", "echo new")
	target, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := srv.previewEnvironmentConfig(target)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChangesPending || got.RestartRequired || len(got.StopResources) != 0 || len(got.RestartResources) != 0 {
		t.Fatalf("preview invented an apply transition: %+v", got)
	}
	if stale, reason := srv.configStale(); stale {
		t.Fatalf("real apply would differ: %s", reason)
	}
	// Preserve the real admission fast path even when bytes change at the same stamp.
	stamp, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte("version: \"3\"\nservices:\n  api:\n    command: echo new-value\n")
	for len(changed) < len(original) {
		changed = append(changed, ' ')
	}
	if len(changed) != len(original) {
		t.Fatal("fixture must retain source size")
	}
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stamp.ModTime(), stamp.ModTime()); err != nil {
		t.Fatal(err)
	}
	target, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err = srv.previewEnvironmentConfig(target)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChangesPending || len(got.StopResources) != 0 {
		t.Fatalf("preview bypassed apply stamp check: %+v", got)
	}
	if stale, _ := srv.configStale(); stale {
		t.Fatal("apply admission differs")
	}
}
