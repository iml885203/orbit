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
)

func TestHandleGraph_HappyPath(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis", Kind: "infra", Image: "redis:7.4"},
		},
		Services: map[string]*config.Service{
			"frontend": {Name: "frontend", Kind: "frontend", DependsOn: []string{"api"}},
			"api":      {Name: "api", Kind: "backend", DependsOn: []string{"redis"}},
		},
	}
	srv := newTestServer(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/graph", nil)
	w := httptest.NewRecorder()
	srv.handleGraph(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp GraphResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Nodes) != 3 {
		t.Errorf("nodes = %d, want 3", len(resp.Nodes))
	}
	if len(resp.Edges) != 2 {
		t.Errorf("edges = %d, want 2", len(resp.Edges))
	}

	// frontend→api must be detachable=true, detached=false
	var frontendToAPI *GraphEdge
	for i := range resp.Edges {
		if resp.Edges[i].From == "frontend" && resp.Edges[i].To == "api" {
			frontendToAPI = &resp.Edges[i]
			break
		}
	}
	if frontendToAPI == nil {
		t.Fatal("missing frontend→api edge")
	}
	if !frontendToAPI.Detachable {
		t.Error("frontend→api should be detachable (source is a frontend service)")
	}
	if frontendToAPI.Detached {
		t.Error("frontend→api should not be detached by default")
	}

	// api→redis must be detachable=false
	for _, e := range resp.Edges {
		if e.From == "api" && e.To == "redis" && e.Detachable {
			t.Error("api→redis should not be detachable (api is backend)")
		}
	}
}

func TestBuildGroupInfos(t *testing.T) {
	cfg := &config.Config{
		Groups: map[string]config.Group{
			"platform": {Enabled: true, Services: []string{"api", "frontend"}},
			"sports":   {Enabled: true, Services: []string{"odds-api", "feed-worker"}},
			"empty":    {Enabled: true, Services: []string{}},
			"typo":     {Enabled: true, Services: []string{"does-not-exist"}},
		},
		Services: map[string]*config.Service{
			"api":         {Name: "api"},
			"frontend":    {Name: "frontend"},
			"odds-api":    {Name: "odds-api"},
			"feed-worker": {Name: "feed-worker"},
		},
	}
	groups := buildGroupInfos(cfg)

	// empty + typo both drop to zero — group with no resolvable services is skipped.
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (got %+v)", len(groups), groups)
	}
	// Sorted by name: "platform" < "sports".
	if groups[0].Name != "platform" || groups[1].Name != "sports" {
		t.Errorf("order = [%s, %s], want [platform, sports]", groups[0].Name, groups[1].Name)
	}
	if len(groups[0].Services) != 2 || groups[0].Services[0] != "api" {
		t.Errorf("platform services = %v", groups[0].Services)
	}
}

func TestHandleGraph_ContainerIconsComeFromEnvConfig(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis":     {Name: "redis", Kind: "infra", Image: "redis:7.4"},
			"custom-db": {Name: "custom-db", Kind: "infra", Image: "postgres:16", Icon: "devicon:postgresql"},
			"worker":    {Name: "worker", Kind: "backend", Image: "worker:latest", Icon: "devicon:docker"},
		},
		Services: map[string]*config.Service{},
	}
	nodes := buildGraphNodes(cfg, nil)

	byName := map[string]GraphNode{}
	for _, n := range nodes {
		byName[n.Name] = n
	}
	if got := byName["custom-db"].Icon; got != "devicon:postgresql" {
		t.Errorf("custom-db icon = %q, want env configured icon", got)
	}
	if got := byName["redis"].Icon; got != "" {
		t.Errorf("redis icon = %q, want empty icon when env omits icon", got)
	}
	if got := byName["worker"].Icon; got != "" {
		t.Errorf("worker icon = %q, want no icon for non-infra container", got)
	}
}

func TestBuildGraphNodes_PropagatesDependencyPortConflict(t *testing.T) {
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis", Kind: "infra"},
		},
		Services: map[string]*config.Service{
			"api": {Name: "api", Kind: "backend", DependsOn: []string{"redis"}},
		},
	}
	statuses := map[string]ResourceStatus{
		"redis": {
			Name:          "redis",
			State:         "degraded",
			LogsAvailable: true,
			PortConflict: &ResourcePortConflict{
				Port:           6379,
				Resource:       "redis",
				InspectCommand: "lsof -nP -iTCP:6379 -sTCP:LISTEN",
			},
		},
		"api": {
			Name:                "api",
			State:               "pending",
			PendingDependencies: []string{"redis"},
		},
	}

	nodes := buildGraphNodes(cfg, statuses)
	byName := map[string]GraphNode{}
	for _, node := range nodes {
		byName[node.Name] = node
	}

	conflict := byName["api"].PortConflict
	if conflict == nil {
		t.Fatal("api port conflict = nil, want redis dependency conflict")
	}
	if conflict.Resource != "redis" || conflict.Port != 6379 {
		t.Fatalf("api port conflict = %#v, want redis:6379", conflict)
	}
	if !byName["redis"].LogsAvailable {
		t.Fatal("redis logs_available = false, want buffered log evidence")
	}
	if byName["api"].LogsAvailable {
		t.Fatal("api logs_available = true, dependency evidence must not imply api output")
	}
}

func TestHandleGraph_DetachedEdgeShown(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"frontend": {Name: "frontend", Kind: "frontend", DependsOn: []string{"api"}},
			"api":      {Name: "api", Kind: "backend"},
		},
	}
	srv := newTestServer(t, cfg)
	srv.SetConfigPath("/tmp/test/development.yaml")
	_ = srv.settings.SetEdgeDetached(srv.currentEnvName(), "frontend", "api", true)

	req := httptest.NewRequest(http.MethodGet, "/api/graph", nil)
	w := httptest.NewRecorder()
	srv.handleGraph(w, req)

	var resp GraphResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Edges) != 1 {
		t.Fatalf("edges = %d, want 1 (still shown even when detached)", len(resp.Edges))
	}
	if !resp.Edges[0].Detached {
		t.Error("edge should be marked detached")
	}
}

func TestHandleGraph_WrongMethod(t *testing.T) {
	srv := newTestServer(t, testConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/graph", nil)
	w := httptest.NewRecorder()
	srv.handleGraph(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleEdgeDetach_HappyPath(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"frontend": {Name: "frontend", Kind: "frontend", DependsOn: []string{"api"}},
			"api":      {Name: "api", Kind: "backend"},
		},
	}
	srv := newTestServer(t, cfg)
	// Set a config path so currentEnvName() returns "development".
	// The handler ignores req.Env and derives env from server state.
	srv.SetConfigPath("/tmp/test/development.yaml")

	body := strings.NewReader(`{"env":"development","detached":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/edges/frontend/api", body)
	w := httptest.NewRecorder()
	srv.handleEdgeDetach(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !srv.settings.IsEdgeDetached("development", "frontend", "api") {
		t.Error("edge should be persisted as detached")
	}
}

func TestHandleEdgeDetach_IgnoresClientEnv(t *testing.T) {
	// Even if the client sends a stale env name, the server should use its
	// own current env — the persisted key must reflect the server's env.
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"frontend": {Name: "frontend", Kind: "frontend", DependsOn: []string{"api"}},
			"api":      {Name: "api", Kind: "backend"},
		},
	}
	srv := newTestServer(t, cfg)
	srv.SetConfigPath("/tmp/test/development.yaml") // server env = "development"

	// Client claims env is "stale-env" — server should ignore that.
	body := strings.NewReader(`{"env":"stale-env","detached":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/edges/frontend/api", body)
	w := httptest.NewRecorder()
	srv.handleEdgeDetach(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	// Must be stored under the server's env, not the client-supplied one.
	if srv.settings.IsEdgeDetached("stale-env", "frontend", "api") {
		t.Error("should NOT be stored under client-supplied stale env")
	}
	if !srv.settings.IsEdgeDetached("development", "frontend", "api") {
		t.Error("should be stored under server's current env")
	}
}

func TestHandleEdgeDetach_RejectsBackend(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"api": {Name: "api", Kind: "backend", DependsOn: []string{"redis"}},
		},
		Containers: map[string]*config.Container{
			"redis": {Name: "redis", Kind: "infra"},
		},
	}
	srv := newTestServer(t, cfg)
	srv.SetConfigPath("/tmp/test/development.yaml")

	body := strings.NewReader(`{"detached":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/edges/api/redis", body)
	w := httptest.NewRecorder()
	srv.handleEdgeDetach(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (backend not detachable)", w.Code)
	}
	if srv.settings.IsEdgeDetached("development", "api", "redis") {
		t.Error("edge should not be persisted when rejected")
	}
}

func TestHandleEdgeDetach_RejectsUnknownService(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{}}
	srv := newTestServer(t, cfg)
	srv.SetConfigPath("/tmp/test/development.yaml")

	body := strings.NewReader(`{"detached":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/edges/ghost/api", body)
	w := httptest.NewRecorder()
	srv.handleEdgeDetach(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleEdgeDetach_WrongMethod(t *testing.T) {
	srv := newTestServer(t, testConfig())
	req := httptest.NewRequest(http.MethodGet, "/api/edges/frontend/api", nil)
	w := httptest.NewRecorder()
	srv.handleEdgeDetach(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleEdgeDetach_URLPathForm(t *testing.T) {
	// Verify the new REST path /api/edges/{from}/{to} works — from/to come
	// from the URL and the body only needs {detached: bool}.
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"frontend": {Name: "frontend", Kind: "frontend", DependsOn: []string{"api"}},
			"api":      {Name: "api", Kind: "backend"},
		},
	}
	srv := newTestServer(t, cfg)
	srv.SetConfigPath("/tmp/test/development.yaml")

	body := strings.NewReader(`{"detached":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/edges/frontend/api", body)
	w := httptest.NewRecorder()
	srv.handleEdgeDetach(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !srv.settings.IsEdgeDetached("development", "frontend", "api") {
		t.Error("edge should be persisted as detached via URL path form")
	}
}

func TestHandleEdgeDetach_LegacyBodyFormRemoved(t *testing.T) {
	// The deprecated /api/edges/detach path (from/to in the JSON body) was
	// removed after all clients migrated to PUT /api/edges/{from}/{to}. It
	// must now 400 — not silently detach whatever the body names.
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"frontend": {Name: "frontend", Kind: "frontend", DependsOn: []string{"api"}},
			"api":      {Name: "api", Kind: "backend"},
		},
	}
	srv := newTestServer(t, cfg)
	srv.SetConfigPath("/tmp/test/development.yaml")

	body := strings.NewReader(`{"env":"development","from":"frontend","to":"api","detached":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/edges/detach", body)
	w := httptest.NewRecorder()
	srv.handleEdgeDetach(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for removed legacy form, body=%s", w.Code, w.Body.String())
	}
	if srv.settings.IsEdgeDetached("development", "frontend", "api") {
		t.Error("legacy body form must not persist a detach")
	}
}

func TestHandleGraph_PreviewLoadsOtherEnv(t *testing.T) {
	tmp := t.TempDir()
	currentPath := filepath.Join(tmp, "current.yaml")
	if err := os.WriteFile(currentPath, []byte("version: \"2\"\n"), 0644); err != nil {
		t.Fatalf("write current.yaml: %v", err)
	}
	previewPath := filepath.Join(tmp, "other.yaml")
	previewYAML := `version: "2"
services:
  frontend:
    kind: frontend
    type: node
`
	if err := os.WriteFile(previewPath, []byte(previewYAML), 0644); err != nil {
		t.Fatalf("write other.yaml: %v", err)
	}

	srv := newTestServer(t, &config.Config{})
	srv.SetConfigPath(currentPath)

	req := httptest.NewRequest(http.MethodGet, "/api/graph?env=other", nil)
	w := httptest.NewRecorder()
	srv.handleGraph(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp GraphResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Env != "other" {
		t.Errorf("env = %q, want %q", resp.Env, "other")
	}
	if len(resp.Nodes) != 1 || resp.Nodes[0].Name != "frontend" {
		t.Errorf("nodes = %+v, want [frontend]", resp.Nodes)
	}
	if resp.Nodes[0].State != "pending" {
		t.Errorf("preview state = %q, want pending", resp.Nodes[0].State)
	}
}

func TestHandleGraph_PreviewUnknownEnv(t *testing.T) {
	tmp := t.TempDir()
	currentPath := filepath.Join(tmp, "current.yaml")
	if err := os.WriteFile(currentPath, []byte("version: \"2\"\n"), 0644); err != nil {
		t.Fatalf("write current.yaml: %v", err)
	}
	srv := newTestServer(t, &config.Config{})
	srv.SetConfigPath(currentPath)

	req := httptest.NewRequest(http.MethodGet, "/api/graph?env=nope", nil)
	w := httptest.NewRecorder()
	srv.handleGraph(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestBuildGraphEdges_SyncKindSet(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"frontend": {Name: "frontend", Kind: "frontend", DependsOn: []string{"api"}},
			"api":      {Name: "api", Kind: "backend"},
		},
	}
	s := newTestServer(t, cfg)
	s.SetConfigPath("/tmp/test/foo.yaml")
	edges := buildGraphEdges(cfg, s.settings, s.currentEnvName())
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(edges))
	}
	if edges[0].Kind != EdgeKindSync {
		t.Errorf("Kind = %q, want %q", edges[0].Kind, EdgeKindSync)
	}
	if edges[0].Topic != "" {
		t.Errorf("Topic = %q, want empty for sync edge", edges[0].Topic)
	}
}

func TestBuildGraphNodes_EmitsExternals(t *testing.T) {
	cfg := &config.Config{
		Externals: map[string]*config.External{
			"upstream": {
				Name:  "upstream",
				Label: "Upstream",
				Color: "#5b21b6",
				Kafka: config.KafkaIO{Produces: []string{"upstream.pricing.odds"}},
			},
		},
		Services: map[string]*config.Service{},
	}
	nodes := buildGraphNodes(cfg, nil)
	var ext *GraphNode
	for i := range nodes {
		if nodes[i].Name == "upstream" {
			ext = &nodes[i]
			break
		}
	}
	if ext == nil {
		t.Fatal("external node upstream missing from graph nodes")
	}
	if ext.Kind != "external" {
		t.Errorf("Kind = %q, want external", ext.Kind)
	}
	if ext.Label != "Upstream" {
		t.Errorf("Label = %q, want Upstream", ext.Label)
	}
	if ext.Color != "#5b21b6" {
		t.Errorf("Color = %q, want #5b21b6", ext.Color)
	}
	if ext.Kafka == nil || len(ext.Kafka.Produces) != 1 || ext.Kafka.Produces[0] != "upstream.pricing.odds" {
		t.Errorf("Kafka = %+v, want produces upstream.pricing.odds", ext.Kafka)
	}
}

func TestBuildGraphNodes_ServiceCarriesKafka(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"odds-api": {
				Name: "odds-api",
				Kind: "backend",
				Kafka: config.KafkaIO{
					Produces: []string{"accounts.settlement"},
					Consumes: []string{"single-bet-source"},
				},
			},
		},
	}
	nodes := buildGraphNodes(cfg, nil)
	var s *GraphNode
	for i := range nodes {
		if nodes[i].Name == "odds-api" {
			s = &nodes[i]
			break
		}
	}
	if s == nil {
		t.Fatal("service odds-api missing from graph nodes")
	}
	if s.Kafka == nil {
		t.Fatal("Kafka nil, want non-nil")
	}
	if len(s.Kafka.Produces) != 1 || s.Kafka.Produces[0] != "accounts.settlement" {
		t.Errorf("Produces = %v", s.Kafka.Produces)
	}
	if len(s.Kafka.Consumes) != 1 || s.Kafka.Consumes[0] != "single-bet-source" {
		t.Errorf("Consumes = %v", s.Kafka.Consumes)
	}
}

func TestBuildGraphNodes_ExternalLabelFallsBackToName(t *testing.T) {
	cfg := &config.Config{
		Externals: map[string]*config.External{
			"upstream": {
				Name: "upstream",
				// Label intentionally empty
				Kafka: config.KafkaIO{Produces: []string{"upstream.feed"}},
			},
		},
	}
	nodes := buildGraphNodes(cfg, nil)
	var ext *GraphNode
	for i := range nodes {
		if nodes[i].Name == "upstream" {
			ext = &nodes[i]
			break
		}
	}
	if ext == nil {
		t.Fatal("external node upstream missing")
	}
	if ext.Label != "upstream" {
		t.Errorf("Label = %q, want fallback to name %q", ext.Label, "upstream")
	}
}

func TestBuildGraphNodes_ServiceWithoutKafkaHasNilKafka(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"plain": {Name: "plain", Kind: "backend"},
			// No Kafka block at all
		},
	}
	nodes := buildGraphNodes(cfg, nil)
	var s *GraphNode
	for i := range nodes {
		if nodes[i].Name == "plain" {
			s = &nodes[i]
			break
		}
	}
	if s == nil {
		t.Fatal("service plain missing")
	}
	if s.Kafka != nil {
		t.Errorf("Kafka = %+v, want nil when service declares no kafka", s.Kafka)
	}
}

func TestBuildAsyncEdges_FanOut(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"prod": {Name: "prod", Kafka: config.KafkaIO{Produces: []string{"t"}}},
			"c1":   {Name: "c1", Kafka: config.KafkaIO{Consumes: []string{"t"}}},
			"c2":   {Name: "c2", Kafka: config.KafkaIO{Consumes: []string{"t"}}},
		},
	}
	edges := buildAsyncEdges(cfg)
	if len(edges) != 2 {
		t.Fatalf("edges = %d, want 2 (fan-out)", len(edges))
	}
	for _, e := range edges {
		if e.From != "prod" {
			t.Errorf("From = %q, want prod", e.From)
		}
		if e.Kind != EdgeKindAsync {
			t.Errorf("Kind = %q, want async", e.Kind)
		}
		if e.Topic != "t" {
			t.Errorf("Topic = %q, want t", e.Topic)
		}
	}
}

func TestBuildAsyncEdges_MultiProducer(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"p1": {Name: "p1", Kafka: config.KafkaIO{Produces: []string{"t"}}},
			"p2": {Name: "p2", Kafka: config.KafkaIO{Produces: []string{"t"}}},
			"c":  {Name: "c", Kafka: config.KafkaIO{Consumes: []string{"t"}}},
		},
	}
	edges := buildAsyncEdges(cfg)
	if len(edges) != 2 {
		t.Fatalf("edges = %d, want 2 (multi producer)", len(edges))
	}
}

func TestBuildAsyncEdges_SkipsSelfLoop(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"x": {Name: "x", Kafka: config.KafkaIO{
				Produces: []string{"t"},
				Consumes: []string{"t"},
			}},
		},
	}
	edges := buildAsyncEdges(cfg)
	if len(edges) != 0 {
		t.Errorf("edges = %d, want 0 (self loop should be skipped)", len(edges))
	}
}

func TestBuildAsyncEdges_OrphanTopicNoConsumer(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"prod": {Name: "prod", Kafka: config.KafkaIO{Produces: []string{"t"}}},
		},
	}
	edges := buildAsyncEdges(cfg)
	if len(edges) != 0 {
		t.Errorf("edges = %d, want 0 (no consumers)", len(edges))
	}
}

func TestBuildAsyncEdges_IncludesExternals(t *testing.T) {
	cfg := &config.Config{
		Externals: map[string]*config.External{
			"upstream": {Name: "upstream", Kafka: config.KafkaIO{Produces: []string{"upstream.feed"}}},
		},
		Services: map[string]*config.Service{
			"odds-api": {Name: "odds-api", Kafka: config.KafkaIO{Consumes: []string{"upstream.feed"}}},
		},
	}
	edges := buildAsyncEdges(cfg)
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(edges))
	}
	if edges[0].From != "upstream" || edges[0].To != "odds-api" {
		t.Errorf("edge = %s -> %s, want upstream -> odds-api", edges[0].From, edges[0].To)
	}
}
