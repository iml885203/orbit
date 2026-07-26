package devdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/tunnel"
)

// sqlServerConfig is testConfig plus a sql-server container — the shape every
// ExampleTeam env has, which is what opts an env into the DB workflow.
func sqlServerConfig() *config.Config {
	cfg := testConfig()
	cfg.Containers["sql-server"] = &config.Container{Name: "sql-server", Image: "mssql:2022"}
	return cfg
}

// sqlProjectsConfig is testConfig plus a declared sql_projects target —
// the generic opt-in path: no container named sql-server anywhere.
func sqlProjectsConfig() *config.Config {
	cfg := testConfig()
	cfg.Containers["mydb"] = &config.Container{Name: "mydb", Image: "mssql:2022"}
	cfg.SQLProjects = &config.SQLProjectsConfig{Target: "mydb"}
	return cfg
}

func TestDBWorkflowGate_RejectsWhenNoSQLServer(t *testing.T) {
	s := newTestDBFeature(t, testConfig())

	endpoints := []struct {
		name    string
		method  string
		body    string
		handler http.HandlerFunc
	}{
		{"devdb projects", http.MethodGet, "", s.handleDevDBProjects},
		{"db publish", http.MethodPost, `{"db":"SomeDB"}`, s.handleDBOpPublish},
	}
	for _, ep := range endpoints {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(ep.method, "/api/x", strings.NewReader(ep.body))
		ep.handler(rr, req)
		if rr.Code != http.StatusPreconditionFailed {
			t.Errorf("%s: status = %d, want 412", ep.name, rr.Code)
		}
		var resp daemon.APIResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: decode: %v", ep.name, err)
		}
		if resp.Error != ErrMsgDBNotConfigured {
			t.Errorf("%s: error = %q, want the not-configured message", ep.name, resp.Error)
		}
	}
}

func TestDBWorkflowGate_MetaReportsConfigured(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{"no sql-server", testConfig(), false},
		{"with sql-server", sqlServerConfig(), true},
		{"sql_projects target without sql-server", sqlProjectsConfig(), true},
	} {
		s := newTestDBFeature(t, tc.cfg)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/devdb/meta", nil)
		s.handleDevDBMeta(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", tc.name, rr.Code)
		}
		var meta DevDBMetaResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &meta); err != nil {
			t.Fatalf("%s: decode: %v", tc.name, err)
		}
		if meta.DBConfigured == nil {
			t.Fatalf("%s: db_configured missing — CLI fail-open logic depends on the daemon always reporting it", tc.name)
		}
		if *meta.DBConfigured != tc.want {
			t.Errorf("%s: db_configured = %v, want %v", tc.name, *meta.DBConfigured, tc.want)
		}
	}
}

func TestDBWorkflowGate_DoctorSkipsDBChecksWhenUnconfigured(t *testing.T) {
	s := newTestDBFeature(t, testConfig())
	checks := s.dbWorkflowChecks()
	var sawSkip bool
	for _, c := range checks {
		if c.Name == "DB Workflow" && c.Status == daemon.CheckInfo {
			sawSkip = true
		}
		if c.Name == "WORKSPACE_ROOT" || c.Name == "SQL Image" {
			t.Errorf("unconfigured env still ran db check %q", c.Name)
		}
	}
	if !sawSkip {
		t.Error("expected an informational 'DB Workflow' skip check")
	}
}

func TestDBWorkflowGate_ConfiguredEnvPassesGate(t *testing.T) {
	s := newTestDBFeature(t, sqlServerConfig())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/devdb/projects", nil)
	s.handleDevDBProjects(rr, req)
	// Downstream may fail for unrelated reasons (no example root in the test
	// env) — the contract here is only that the gate does not fire.
	if rr.Code == http.StatusPreconditionFailed {
		t.Errorf("configured env hit the not-configured gate: %s", rr.Body.String())
	}
}

// The Tunnels tab gates on claim_configured, reported by the same
// devMeta endpoint (spec tail A). Fail-open pointer, mirroring
// db_configured: an env with a claim section reports true, one without
// reports false, and the field is always present so the UI never has to
// guess.
func TestClaimGate_MetaReportsConfigured(t *testing.T) {
	claimEnv := (&config.Config{}).WithExtension("claim", &tunnel.ClaimConfig{Gateway: "https://tunlease.example"})
	for _, tc := range []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{"no claim", testConfig(), false},
		{"with claim", claimEnv, true},
	} {
		s := newTestDBFeature(t, tc.cfg)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/devdb/meta", nil)
		s.handleDevDBMeta(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", tc.name, rr.Code)
		}
		var meta DevDBMetaResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &meta); err != nil {
			t.Fatalf("%s: decode: %v", tc.name, err)
		}
		if meta.ClaimConfigured == nil {
			t.Fatalf("%s: claim_configured missing — the Tunnels tab gate depends on the daemon always reporting it", tc.name)
		}
		if *meta.ClaimConfigured != tc.want {
			t.Errorf("%s: claim_configured = %v, want %v", tc.name, *meta.ClaimConfigured, tc.want)
		}
	}
}
