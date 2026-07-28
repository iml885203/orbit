package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

func TestBuildInspectServiceSummaryGroupsStates(t *testing.T) {
	got := buildInspectServiceSummary([]daemon.ResourceStatus{
		{Name: "payments", State: "stopped"},
		{Name: "redis", State: "healthy"},
		{Name: "worker", State: "degraded"},
		{Name: "api", State: "starting"},
	})

	if got.Total != 4 {
		t.Fatalf("total = %d, want 4", got.Total)
	}
	assertStringSlice(t, got.ByState["degraded"], []string{"worker"})
	assertStringSlice(t, got.ByState["healthy"], []string{"redis"})
	assertStringSlice(t, got.ByState["starting"], []string{"api"})
	assertStringSlice(t, got.ByState["stopped"], []string{"payments"})
	assertStringSlice(t, got.Degraded, []string{"worker"})
	assertStringSlice(t, got.Starting, []string{"api"})
	assertStringSlice(t, got.Stopped, []string{"payments"})
}

func TestBuildInspectDataConfigInvalidWins(t *testing.T) {
	got := buildInspectData(inspectBuildOptions{
		ConfigPath: "/tmp/missing.yaml",
		ConfigErr:  errInspectFixture("open /tmp/missing.yaml: no such file or directory"),
	})

	if got.Readiness.State != inspectReadinessConfigInvalid {
		t.Fatalf("state = %q, want %q", got.Readiness.State, inspectReadinessConfigInvalid)
	}
	if !got.Readiness.Blocked {
		t.Fatal("blocked = false, want true")
	}
	if len(got.Risks) != 1 || got.Risks[0].Code != "config_invalid" {
		t.Fatalf("risks = %+v", got.Risks)
	}
	if got.RecommendedActions[0].Command != "orbit inspect --json" {
		t.Fatalf("first action = %+v", got.RecommendedActions[0])
	}
}

func TestBuildInspectDataSetupRequiredPointsOnlyToInit(t *testing.T) {
	got := buildInspectData(inspectBuildOptions{
		Command:       "orbit inspect --json",
		ConfigPath:    "/tmp/missing.yaml",
		ConfigErr:     errInspectFixture("no such file or directory"),
		SetupRequired: true,
		ConfigEnvName: "quickstart",
		DaemonRunning: false,
	})

	if got.Readiness.State != inspectReadinessSetupRequired || !got.Readiness.Blocked {
		t.Fatalf("readiness = %+v", got.Readiness)
	}
	if got.Environment.SelectedName != "" || got.Environment.SelectedPath != "" {
		t.Fatalf("environment = %+v, want no pretend selection before setup", got.Environment)
	}
	if len(got.Risks) != 1 || got.Risks[0].Code != "setup_required" {
		t.Fatalf("risks = %+v", got.Risks)
	}
	if len(got.RecommendedActions) != 1 || got.RecommendedActions[0].Command != "orbit init --yes --json" {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestBuildInspectDataUnavailableSelectionOffersExistingEnvironment(t *testing.T) {
	got := buildInspectData(inspectBuildOptions{
		Command:       "orbit inspect --json",
		ConfigPath:    "/tmp/original.yaml",
		ConfigErr:     errInspectFixture("no such file or directory"),
		ConfigEnvName: "original",
		Selection: environmentSelection{
			State:        environmentSelectionUnavailable,
			SelectedName: "original",
			SelectedPath: "/tmp/original.yaml",
			Environments: []environmentChoice{{
				Name: "renamed",
				Path: "/tmp/renamed.yaml",
			}},
		},
	})

	if got.Readiness.State != inspectReadinessSelectionRequired || !got.Readiness.Blocked {
		t.Fatalf("readiness = %+v", got.Readiness)
	}
	if len(got.Risks) != 1 || got.Risks[0].Code != "environment_selection_required" {
		t.Fatalf("risks = %+v", got.Risks)
	}
	if got.Environment.State != environmentSelectionUnavailable ||
		len(got.Environment.Environments) != 1 {
		t.Fatalf("environment = %+v", got.Environment)
	}
	if len(got.RecommendedActions) != 1 ||
		got.RecommendedActions[0].Command != "orbit switch renamed --json" {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestBuildInspectDataDaemonDown(t *testing.T) {
	got := buildInspectData(inspectBuildOptions{
		ConfigPath:    "/tmp/development.yaml",
		ConfigEnvName: "development",
		DaemonRunning: false,
		Configured: []daemon.ResourceStatus{
			{Name: "redis", Kind: daemon.ResourceKindContainer, State: "stopped"},
			{Name: "api", Kind: daemon.ResourceKindService, State: "stopped"},
		},
	})

	if got.Readiness.State != inspectReadinessStopped {
		t.Fatalf("state = %q, want %q", got.Readiness.State, inspectReadinessStopped)
	}
	if !got.Readiness.Blocked {
		t.Fatal("blocked = false, want true")
	}
	if len(got.Risks) != 1 || got.Risks[0].Code != "environment_stopped" {
		t.Fatalf("risks = %+v", got.Risks)
	}
	if got.Resources.Total != 2 || len(got.Resources.Stopped) != 2 {
		t.Fatalf("resources = %+v", got.Resources)
	}
	if len(got.RecommendedActions) != 1 || got.RecommendedActions[0].Command != "orbit up --json" {
		t.Fatalf("first action = %+v", got.RecommendedActions[0])
	}
}

func TestBuildInspectDataInstalledUpdateRequiresRestart(t *testing.T) {
	got := buildInspectData(inspectBuildOptions{
		ConfigPath:      "/tmp/development.yaml",
		ConfigEnvName:   "development",
		DaemonRunning:   true,
		UpdateAvailable: true,
		Status: &daemon.StatusResponse{Resources: []daemon.ResourceStatus{{
			Name:  "api",
			State: "stopped",
		}}},
		Selection: environmentSelection{
			State:        environmentSelectionSelected,
			SelectedName: "development",
			SelectedPath: "/tmp/development.yaml",
			Environments: []environmentChoice{{
				Name:     "development",
				Path:     "/tmp/development.yaml",
				Selected: true,
			}},
		},
	})

	if got.Readiness.State != inspectReadinessUpdateRequired || !got.Readiness.Blocked {
		t.Fatalf("readiness = %+v", got.Readiness)
	}
	if len(got.Risks) != 1 || got.Risks[0].Code != "orbit_update_pending" {
		t.Fatalf("risks = %+v", got.Risks)
	}
	if len(got.RecommendedActions) != 1 ||
		got.RecommendedActions[0].Command != "orbit daemon restart --json" {
		t.Fatalf("recommended_actions = %+v", got.RecommendedActions)
	}
}

func TestConfiguredInspectServicesExposeStoppedResourcesWithoutDaemon(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{"redis": {}},
		Services:   map[string]*config.Service{"api": {}},
	}
	got := buildInspectServiceSummary(configuredInspectServices(cfg))
	if got.Total != 2 {
		t.Fatalf("total = %d", got.Total)
	}
	assertStringSlice(t, got.Stopped, []string{"api", "redis"})
}

func TestBuildInspectDataDaemonEnvMismatchBlocksReady(t *testing.T) {
	got := buildInspectData(inspectBuildOptions{
		ConfigPath:    "/tmp/development.yaml",
		ConfigEnvName: "development",
		DaemonRunning: true,
		DaemonEnv:     "local",
		Status: &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
			{Name: "redis", State: "healthy"},
		}},
	})

	if got.Readiness.State != inspectReadinessNeedsDaemon {
		t.Fatalf("state = %q, want %q", got.Readiness.State, inspectReadinessNeedsDaemon)
	}
	if !got.Readiness.Blocked {
		t.Fatal("blocked = false, want true")
	}
	if len(got.Risks) != 1 || got.Risks[0].Code != "env_mismatch" {
		t.Fatalf("risks = %+v", got.Risks)
	}
	if got.Risks[0].Severity != "critical" {
		t.Fatalf("risk severity = %q, want critical", got.Risks[0].Severity)
	}
	if !strings.Contains(got.Risks[0].Message, "development") || !strings.Contains(got.Risks[0].Message, "local") {
		t.Fatalf("risk message = %q, want both env names", got.Risks[0].Message)
	}
	if len(got.RecommendedActions) == 0 || got.RecommendedActions[0].Command != "orbit switch development --json" {
		t.Fatalf("first action = %+v", got.RecommendedActions)
	}
	if got.RecommendedActions[0].Reason != "Apply the selected environment through Orbit's safe switch workflow." {
		t.Fatalf("first action reason = %q", got.RecommendedActions[0].Reason)
	}
	if got.RecommendedActions[0].Destructive {
		t.Fatal("first action destructive = true, want false")
	}
}

func TestBuildInspectDataZeroServicesMarshalEmptyArrays(t *testing.T) {
	data := buildInspectData(inspectBuildOptions{
		ConfigPath:    "/tmp/development.yaml",
		ConfigEnvName: "development",
		DaemonRunning: false,
	})

	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal inspect data: %v", err)
	}
	body := string(raw)
	for _, field := range []string{"degraded", "starting", "stopped"} {
		if strings.Contains(body, `"`+field+`":null`) {
			t.Fatalf("%s marshaled as null: %s", field, body)
		}
		if !strings.Contains(body, `"`+field+`":[]`) {
			t.Fatalf("%s missing empty array: %s", field, body)
		}
	}
}

func TestBuildInspectDataDegradedBeatsStopped(t *testing.T) {
	got := buildInspectData(inspectBuildOptions{
		ConfigPath:    "/tmp/development.yaml",
		ConfigEnvName: "development",
		DaemonRunning: true,
		Status: &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
			{Name: "worker", State: "degraded"},
			{Name: "payments", State: "stopped"},
		}},
	})

	if got.Readiness.State != inspectReadinessDegraded {
		t.Fatalf("state = %q, want %q", got.Readiness.State, inspectReadinessDegraded)
	}
	if got.Readiness.Blocked {
		t.Fatal("blocked = true, want false")
	}
	if len(got.Risks) != 2 {
		t.Fatalf("risks = %+v, want 2 risks", got.Risks)
	}
	if got.Risks[0].Code != "resource_degraded" || got.Risks[0].Resource != "worker" {
		t.Fatalf("first risk = %+v", got.Risks[0])
	}
	if got.RecommendedActions[0].Command != "orbit logs worker --json" {
		t.Fatalf("first action = %+v", got.RecommendedActions[0])
	}
	if len(got.RecommendedActions) != 1 {
		t.Fatalf("recommended actions = %+v, want one linear recovery step", got.RecommendedActions)
	}
	if hasInspectAction(got.RecommendedActions, "orbit doctor --json") {
		t.Fatalf("recommended actions include unrelated setup diagnostics: %+v", got.RecommendedActions)
	}
}

func TestBuildInspectDataStatusUnavailableIsNotReady(t *testing.T) {
	got := buildInspectData(inspectBuildOptions{
		ConfigPath:    "/tmp/development.yaml",
		ConfigEnvName: "development",
		DaemonRunning: true,
		StatusErr:     errInspectFixture("status request failed: connection reset"),
	})

	if got.Readiness.State != inspectReadinessConverging {
		t.Fatalf("state = %q, want %q", got.Readiness.State, inspectReadinessConverging)
	}
	if got.Readiness.Blocked {
		t.Fatal("blocked = true, want false")
	}
	if len(got.Risks) != 1 || got.Risks[0].Code != "status_unavailable" {
		t.Fatalf("risks = %+v", got.Risks)
	}
	if !hasInspectAction(got.RecommendedActions, "orbit status --json") {
		t.Fatalf("recommended actions missing status: %+v", got.RecommendedActions)
	}
	if !hasInspectAction(got.RecommendedActions, "orbit doctor --json") {
		t.Fatalf("recommended actions missing doctor: %+v", got.RecommendedActions)
	}
}

func TestBuildInspectDataStoppingAndRestartingAreConverging(t *testing.T) {
	got := buildInspectData(inspectBuildOptions{
		ConfigPath:    "/tmp/development.yaml",
		ConfigEnvName: "development",
		DaemonRunning: true,
		Status: &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
			{Name: "worker", State: "restarting"},
			{Name: "payments", State: "stopping"},
		}},
	})

	if got.Readiness.State != inspectReadinessConverging {
		t.Fatalf("state = %q, want %q", got.Readiness.State, inspectReadinessConverging)
	}
	assertStringSlice(t, got.Resources.Starting, []string{"payments", "worker"})
	assertStringSlice(t, got.Resources.ByState["restarting"], []string{"worker"})
	assertStringSlice(t, got.Resources.ByState["stopping"], []string{"payments"})
}

func TestBuildInspectDataEnvelopePayload(t *testing.T) {
	data := buildInspectData(inspectBuildOptions{
		ConfigPath:    "/tmp/development.yaml",
		ConfigEnvName: "development",
		DaemonRunning: true,
		PID:           123,
		Version:       "dev",
		Dashboard:     "http://localhost:7171",
		Status: &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
			{Name: "redis", State: "healthy"},
		}},
	})

	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal inspect data: %v", err)
	}
	var got struct {
		SchemaVersion string `json:"schema_version"`
		Readiness     struct {
			State string `json:"state"`
		} `json:"readiness"`
		Daemon struct {
			Running bool   `json:"running"`
			PID     int    `json:"pid"`
			Version string `json:"version"`
		} `json:"daemon"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal inspect data: %v\n%s", err, raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal inspect fields: %v", err)
	}
	if _, ok := fields["resources"]; !ok {
		t.Fatalf("resources missing from %s", raw)
	}
	if _, ok := fields["services"]; ok {
		t.Fatalf("legacy services field present in %s", raw)
	}
	if got.SchemaVersion != inspectJSONSchemaVersion {
		t.Fatalf("schema_version = %q", got.SchemaVersion)
	}
	if got.Readiness.State != inspectReadinessReady {
		t.Fatalf("readiness.state = %q", got.Readiness.State)
	}
	if !got.Daemon.Running || got.Daemon.PID != 123 || got.Daemon.Version != "dev" {
		t.Fatalf("daemon = %+v", got.Daemon)
	}
}

type errInspectFixture string

func (e errInspectFixture) Error() string { return string(e) }

func hasInspectAction(actions []cli.JSONAction, command string) bool {
	for _, action := range actions {
		if action.Command == command {
			return true
		}
	}
	return false
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slice = %+v, want %+v", got, want)
		}
	}
}
