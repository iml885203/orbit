package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

type statusJSON struct {
	Daemon    daemonStatus  `json:"daemon"`
	Resources []jsonService `json:"resources"`
}

type renderedStatusEnvelope struct {
	SchemaVersion      string           `json:"schema_version"`
	OK                 bool             `json:"ok"`
	Command            string           `json:"command"`
	Data               statusJSON       `json:"data"`
	RecommendedActions []cli.JSONAction `json:"recommended_actions"`
}

func renderStatusEnvelope(t *testing.T, cfg *config.Config, running map[string]daemon.ResourceStatus, d daemonStatus) renderedStatusEnvelope {
	t.Helper()
	var buf bytes.Buffer
	if err := writeStatusJSON(&buf, "orbit status --json", cfg, running, d, statusSetupState{}); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}
	var envelope renderedStatusEnvelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, buf.String())
	}
	return envelope
}

func renderStatusJSON(t *testing.T, cfg *config.Config, running map[string]daemon.ResourceStatus, d daemonStatus) statusJSON {
	t.Helper()
	envelope := renderStatusEnvelope(t, cfg, running, d)
	if envelope.SchemaVersion != "orbit.cli.v1" {
		t.Errorf("schema_version: got %q", envelope.SchemaVersion)
	}
	if !envelope.OK {
		t.Error("ok: got false")
	}
	if envelope.Command != "orbit status --json" {
		t.Errorf("command: got %q", envelope.Command)
	}
	return envelope.Data
}

func TestStatusJSONUsesResourceVocabulary(t *testing.T) {
	var buf bytes.Buffer
	if err := writeStatusJSON(
		&buf,
		"orbit status --json",
		&config.Config{},
		nil,
		daemonStatus{},
		statusSetupState{},
	); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := envelope.Data["resources"]; !ok {
		t.Fatalf("resources missing from %s", buf.String())
	}
	if _, ok := envelope.Data["services"]; ok {
		t.Fatalf("legacy services field present in %s", buf.String())
	}
}

func TestStatusJSON_DegradedServiceExplainsAndRepairs(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"worker": {}}}
	running := map[string]daemon.ResourceStatus{
		"worker": {
			Name:        "worker",
			State:       "degraded",
			StateReason: "exited: exit status 17",
		},
	}
	envelope := renderStatusEnvelope(t, cfg, running, daemonStatus{Running: true})
	service := envelope.Data.Resources[0]
	if service.StateReason != "exited: exit status 17" {
		t.Fatalf("state_reason = %q", service.StateReason)
	}
	want := []string{"orbit logs worker --json", "orbit restart worker --json"}
	if len(envelope.RecommendedActions) != len(want) {
		t.Fatalf("recommended_actions = %+v, want %v", envelope.RecommendedActions, want)
	}
	for i, command := range want {
		if envelope.RecommendedActions[i].Command != command {
			t.Fatalf("recommended_actions[%d] = %q, want %q", i, envelope.RecommendedActions[i].Command, command)
		}
	}
}

func TestStatusPortConflictRecommendsInspectionInsteadOfRestart(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{"redis": {}},
		Services:   map[string]*config.Service{"api": {DependsOn: []string{"redis"}}},
	}
	running := map[string]daemon.ResourceStatus{
		"redis": {
			Name:        "redis",
			Kind:        daemon.ResourceKindContainer,
			State:       "degraded",
			StateReason: "cannot start redis: port 26379 is already in use",
			PortConflict: &daemon.ResourcePortConflict{
				Port:           26379,
				Resource:       "redis",
				PID:            "42",
				InspectCommand: "ps -p 42 -o pid,comm,args=",
			},
		},
		"api": {
			Name:                "api",
			Kind:                daemon.ResourceKindService,
			State:               "pending",
			PendingDependencies: []string{"redis"},
		},
	}
	envelope := renderStatusEnvelope(t, cfg, running, daemonStatus{Running: true})
	if len(envelope.RecommendedActions) != 1 ||
		envelope.RecommendedActions[0].Command != "ps -p 42 -o pid,comm,args=" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
	for _, action := range envelope.RecommendedActions {
		if strings.Contains(action.Command, "logs") || strings.Contains(action.Command, "restart") {
			t.Fatalf("port conflict recommends blind retry: %+v", envelope.RecommendedActions)
		}
	}
}

func TestStatusJSON_PendingServiceNamesRootBlocker(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{
		"api":   {},
		"redis": {},
	}}
	running := map[string]daemon.ResourceStatus{
		"api": {
			Name:                "api",
			State:               "pending",
			PendingDependencies: []string{"redis"},
		},
		"redis": {
			Name:        "redis",
			State:       "degraded",
			StateReason: "container exited unexpectedly",
		},
	}
	envelope := renderStatusEnvelope(t, cfg, running, daemonStatus{Running: true})
	var api jsonService
	for _, service := range envelope.Data.Resources {
		if service.Name == "api" {
			api = service
		}
	}
	if api.BlockedBy != "redis" {
		t.Fatalf("blocked_by = %q, want redis", api.BlockedBy)
	}
	if len(api.PendingDependencies) != 1 || api.PendingDependencies[0] != "redis" {
		t.Fatalf("pending_dependencies = %v", api.PendingDependencies)
	}
	for _, action := range envelope.RecommendedActions {
		if action.Command == "orbit logs api --json" || action.Command == "orbit restart api --json" {
			t.Fatalf("recommended_actions includes non-actionable dependent command: %+v", envelope.RecommendedActions)
		}
	}
}

func TestStatusDetail_ExplainsFailureAndDependencyBlock(t *testing.T) {
	running := map[string]daemon.ResourceStatus{
		"api": {
			Name:                "api",
			State:               "pending",
			PendingDependencies: []string{"redis"},
		},
		"redis": {
			Name:        "redis",
			State:       "degraded",
			StateReason: "exited: exit status 17",
		},
	}
	if got := statusDetail(running["redis"], running); got != "exited: exit status 17" {
		t.Fatalf("degraded detail = %q", got)
	}
	if got := statusDetail(running["api"], running); got != "blocked by redis — exited: exit status 17" {
		t.Fatalf("pending detail = %q", got)
	}
}

func TestStatusJSON_DaemonStopped(t *testing.T) {
	var buf bytes.Buffer
	if err := writeStatusJSON(
		&buf,
		"orbit status --json",
		&config.Config{},
		nil,
		daemonStatus{Running: false},
		statusSetupState{},
	); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}
	var envelope struct {
		Data struct {
			Daemon    daemonStatus  `json:"daemon"`
			Resources []jsonService `json:"resources"`
		} `json:"data"`
		RecommendedActions []struct {
			Command string `json:"command"`
		} `json:"recommended_actions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := envelope.Data
	if got.Daemon.Running {
		t.Errorf("Running: got true, want false")
	}
	if got.Daemon.Version != "" {
		t.Errorf("Version: got %q, want empty", got.Daemon.Version)
	}
	if got.Daemon.UpdateAvailable {
		t.Errorf("UpdateAvailable: got true, want false")
	}
	if got.Resources == nil {
		t.Error("resources: got null, want empty array")
	}
	if len(envelope.RecommendedActions) != 1 || envelope.RecommendedActions[0].Command != "orbit up --json" {
		t.Errorf("recommended_actions: got %+v", envelope.RecommendedActions)
	}
}

func TestStatusJSON_SetupRequiredRecommendsInitInsteadOfUp(t *testing.T) {
	var buf bytes.Buffer
	setup := statusSetupState{
		Required: true,
		Message:  "No usable environment is selected. Run Orbit setup first.",
	}
	if err := writeStatusJSON(&buf, "orbit status --json", nil, nil, daemonStatus{}, setup); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}
	var envelope struct {
		Data struct {
			SetupRequired bool   `json:"setup_required"`
			SetupMessage  string `json:"setup_message"`
		} `json:"data"`
		RecommendedActions []cli.JSONAction `json:"recommended_actions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !envelope.Data.SetupRequired || envelope.Data.SetupMessage != setup.Message {
		t.Fatalf("setup data = %+v", envelope.Data)
	}
	if len(envelope.RecommendedActions) != 1 || envelope.RecommendedActions[0].Command != "orbit init --yes --json" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
}

func TestStatusJSON_UnavailableSelectionOffersExistingEnvironments(t *testing.T) {
	var buf bytes.Buffer
	setup := statusSetupState{
		SelectionRequired: true,
		Message:           `Environment "original" is no longer available.`,
		Selection: environmentSelection{
			State:        environmentSelectionUnavailable,
			SelectedName: "original",
			SelectedPath: "/tmp/original.yaml",
			Environments: []environmentChoice{{
				Name: "renamed",
				Path: "/tmp/renamed.yaml",
			}},
		},
	}
	running := map[string]daemon.ResourceStatus{
		"api": {Name: "api", Kind: daemon.ResourceKindService, State: "healthy"},
	}
	if err := writeStatusJSON(
		&buf,
		"orbit status --json",
		nil,
		running,
		daemonStatus{Running: true},
		setup,
	); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}
	var envelope struct {
		Data struct {
			SetupRequired     bool                 `json:"setup_required"`
			SelectionRequired bool                 `json:"selection_required"`
			SetupMessage      string               `json:"setup_message"`
			SelectionMessage  string               `json:"selection_message"`
			Environment       environmentSelection `json:"environment"`
			Resources         []jsonService        `json:"resources"`
		} `json:"data"`
		RecommendedActions []cli.JSONAction `json:"recommended_actions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.Data.SetupRequired || !envelope.Data.SelectionRequired {
		t.Fatalf("selection flags = %+v", envelope.Data)
	}
	if envelope.Data.SetupMessage != "" || envelope.Data.SelectionMessage != setup.Message {
		t.Fatalf("selection messages = %+v", envelope.Data)
	}
	if envelope.Data.Environment.State != environmentSelectionUnavailable {
		t.Fatalf("environment = %+v", envelope.Data.Environment)
	}
	if len(envelope.Data.Resources) != 1 || envelope.Data.Resources[0].Name != "api" {
		t.Fatalf("resources = %+v", envelope.Data.Resources)
	}
	if len(envelope.RecommendedActions) != 1 ||
		envelope.RecommendedActions[0].Command != "orbit switch renamed --json" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
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

func TestStatusAfterDownRecommendsWholeEnvironment(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{"redis": {}},
		Services:   map[string]*config.Service{"api": {}},
	}
	running := map[string]daemon.ResourceStatus{
		"redis": {Name: "redis", Kind: daemon.ResourceKindContainer, State: "stopped"},
		"api":   {Name: "api", Kind: daemon.ResourceKindService, State: "stopped"},
	}

	tips := buildTips(true, true, []string{"api"}, nil, nil)
	if len(tips) != 1 || tips[0] != "orbit up                  start environment" {
		t.Fatalf("tips = %+v", tips)
	}

	envelope := renderStatusEnvelope(t, cfg, running, daemonStatus{Running: true})
	if len(envelope.RecommendedActions) != 1 ||
		envelope.RecommendedActions[0].Command != "orbit up --json" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
}

func TestStatusDoesNotPromoteInfrastructureOnlyStartup(t *testing.T) {
	tips := buildTips(true, true, nil, nil, nil)
	if len(tips) != 1 || tips[0] != "orbit up                  start environment" {
		t.Fatalf("tips = %+v", tips)
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
	envelope := renderStatusEnvelope(t, nil, nil, d)
	if len(envelope.RecommendedActions) != 1 ||
		envelope.RecommendedActions[0].Command != "orbit daemon restart --json" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
}

func TestStatusJSON_ConfigStaleShowsRunningSnapshotAndOnlyRestart(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{
		"desired-new-name": {},
	}}
	running := map[string]daemon.ResourceStatus{
		"running-old-name": {
			Name:  "running-old-name",
			Kind:  daemon.ResourceKindService,
			State: "stopped",
			URL:   "http://localhost:4321",
		},
	}
	envelope := renderStatusEnvelope(t, cfg, running, daemonStatus{
		Running:           true,
		ConfigStale:       true,
		ConfigStaleReason: "env file edited",
	})

	if len(envelope.Data.Resources) != 1 ||
		envelope.Data.Resources[0].Name != "running-old-name" {
		t.Fatalf("resources = %+v, want running daemon snapshot", envelope.Data.Resources)
	}
	if len(envelope.RecommendedActions) != 1 ||
		envelope.RecommendedActions[0].Command != "orbit daemon restart --json" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
}

// Guards against regression to pre-migration schema where daemon was a bool.
func TestStatusJSON_DaemonIsObjectNotBool(t *testing.T) {
	var buf bytes.Buffer
	if err := writeStatusJSON(&buf, "orbit status --json", nil, nil, daemonStatus{Running: true}, statusSetupState{}); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := raw["data"]
	if !ok {
		t.Fatal("data field missing")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("data not an object: %v", err)
	}
	d, ok := payload["daemon"]
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

func TestStatusJSON_ResourcesFromConfigAndRunning(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Ports: map[string]config.PortDef{"cli": {Host: 16379}}},
		},
		Services: map[string]*config.Service{
			"worker": {URL: "https://localhost:7144"},
		},
	}
	running := map[string]daemon.ResourceStatus{
		"redis": {Name: "redis", State: "healthy"},
	}
	got := renderStatusJSON(t, cfg, running, daemonStatus{Running: true})

	if len(got.Resources) != 2 {
		t.Fatalf("Resources count: got %d, want 2", len(got.Resources))
	}
	var redis, worker *jsonService
	for i := range got.Resources {
		switch got.Resources[i].Name {
		case "redis":
			redis = &got.Resources[i]
		case "worker":
			worker = &got.Resources[i]
		}
	}
	if redis == nil || redis.State != "healthy" || redis.Kind != "container" {
		t.Errorf("redis entry wrong: %+v", redis)
	}
	if worker == nil || worker.State != "stopped" || worker.Kind != "service" {
		t.Errorf("worker entry wrong: %+v", worker)
	}
}
