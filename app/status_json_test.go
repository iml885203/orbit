package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

type statusJSON struct {
	Daemon   daemonStatus  `json:"daemon"`
	Services []jsonService `json:"services"`
}

func renderStatusJSON(t *testing.T, cfg *config.Config, running map[string]daemon.ServiceStatus, d daemonStatus) statusJSON {
	t.Helper()
	var buf bytes.Buffer
	if err := writeStatusJSON(&buf, cfg, running, d); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}
	var got statusJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, buf.String())
	}
	return got
}

func TestStatusJSON_DaemonStopped(t *testing.T) {
	got := renderStatusJSON(t, nil, nil, daemonStatus{Running: false})
	if got.Daemon.Running {
		t.Errorf("Running: got true, want false")
	}
	if got.Daemon.Version != "" {
		t.Errorf("Version: got %q, want empty", got.Daemon.Version)
	}
	if got.Daemon.UpdateAvailable {
		t.Errorf("UpdateAvailable: got true, want false")
	}
}

func TestStatusJSON_DaemonRunning(t *testing.T) {
	d := daemonStatus{
		Running:         true,
		Version:         "b72a1f7 (2026-04-18T12:00:00Z)",
		UpdateAvailable: false,
	}
	got := renderStatusJSON(t, nil, nil, d)
	if !got.Daemon.Running {
		t.Error("Running: got false, want true")
	}
	if got.Daemon.Version != d.Version {
		t.Errorf("Version: got %q, want %q", got.Daemon.Version, d.Version)
	}
	if got.Daemon.OnDisk != "" {
		t.Errorf("OnDisk: got %q, want empty", got.Daemon.OnDisk)
	}
	if got.Daemon.UpdateAvailable {
		t.Error("UpdateAvailable: got true, want false")
	}
}

func TestStatusJSON_UpdateAvailable(t *testing.T) {
	d := daemonStatus{
		Running:         true,
		Version:         "b72a1f7 (2026-04-18T12:00:00Z)",
		OnDisk:          "c9a3e2b (2026-04-18T13:30:00Z)",
		UpdateAvailable: true,
	}
	got := renderStatusJSON(t, nil, nil, d)
	if got.Daemon.OnDisk != d.OnDisk {
		t.Errorf("OnDisk: got %q, want %q", got.Daemon.OnDisk, d.OnDisk)
	}
	if !got.Daemon.UpdateAvailable {
		t.Error("UpdateAvailable: got false, want true")
	}
}

// Guards against regression to pre-migration schema where daemon was a bool.
func TestStatusJSON_DaemonIsObjectNotBool(t *testing.T) {
	var buf bytes.Buffer
	if err := writeStatusJSON(&buf, nil, nil, daemonStatus{Running: true}); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	d, ok := raw["daemon"]
	if !ok {
		t.Fatal("daemon field missing")
	}
	var asBool bool
	if err := json.Unmarshal(d, &asBool); err == nil {
		t.Errorf("daemon decoded as bool (%v) — should be object", asBool)
	}
	var asObj map[string]any
	if err := json.Unmarshal(d, &asObj); err != nil {
		t.Errorf("daemon not an object: %v", err)
	}
	for _, key := range []string{"running", "update_available"} {
		if _, ok := asObj[key]; !ok {
			t.Errorf("daemon.%s missing", key)
		}
	}
}

func TestStatusJSON_ServicesFromConfigAndRunning(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Ports: map[string]config.PortDef{"cli": {Host: 16379}}},
		},
		Services: map[string]*config.Service{
			"worker": {URL: "https://localhost:7144"},
		},
	}
	running := map[string]daemon.ServiceStatus{
		"redis": {Name: "redis", State: "healthy"},
	}
	got := renderStatusJSON(t, cfg, running, daemonStatus{Running: true})

	if len(got.Services) != 2 {
		t.Fatalf("Services count: got %d, want 2", len(got.Services))
	}
	var redis, worker *jsonService
	for i := range got.Services {
		switch got.Services[i].Name {
		case "redis":
			redis = &got.Services[i]
		case "worker":
			worker = &got.Services[i]
		}
	}
	if redis == nil || redis.State != "healthy" || redis.Kind != "container" {
		t.Errorf("redis entry wrong: %+v", redis)
	}
	if worker == nil || worker.State != "stopped" || worker.Kind != "service" {
		t.Errorf("worker entry wrong: %+v", worker)
	}
}
