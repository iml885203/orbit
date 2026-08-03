package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/env"
)

// sortedKeys returns the keys of m in stable alphabetical order so the graph
// response is deterministic — Go map iteration is randomised, which made
// dagre re-layout the canvas every poll tick.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// GraphResponse is the response for GET /api/graph.
type GraphResponse struct {
	Env string `json:"env"`
	// Groups maps group name → service names declared in that group. The UI
	// uses it to cluster service nodes by group.
	// Only groups with at least one service are included. Order is
	// deterministic (sorted by group name) so the canvas doesn't reshuffle
	// on each poll.
	Groups []GroupInfo `json:"groups,omitempty"`
	Nodes  []GraphNode `json:"nodes"`
	Edges  []GraphEdge `json:"edges"`
}

// GroupInfo is one entry in GraphResponse.Groups. Kept as a slice (not a
// map) so the order is deterministic without the UI having to re-sort.
// Color forwards the yaml-provided color so the UI can theme each group
// box; empty means "derive a stable hue from the name on the client".
type GroupInfo struct {
	Name     string   `json:"name"`
	Color    string   `json:"color,omitempty"`
	Services []string `json:"services"`
}

// GraphNode is one node in the dependency graph. Kind here is the display
// category (frontend|backend|infra), not the daemon's topology Kind which
// is "service" vs "container".
type GraphNode struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"` // frontend | backend | infra | external
	Icon  string `json:"icon,omitempty"`
	Label string `json:"label,omitempty"` // externals only; display name
	Color string `json:"color,omitempty"` // externals only; hex color tint
	State string `json:"state"`
	// StateReason says why the node is degraded; empty otherwise.
	StateReason          string              `json:"stateReason,omitempty"`
	FailureKind          string              `json:"failureKind,omitempty"`
	BlockedBy            string              `json:"blockedBy,omitempty"`
	PortConflict         *GraphPortConflict  `json:"portConflict,omitempty"`
	LogsAvailable        bool                `json:"logsAvailable,omitempty"`
	Mode                 string              `json:"mode,omitempty"` // services only
	URL                  string              `json:"url,omitempty"`
	Ports                map[string]int      `json:"ports,omitempty"`
	Health               *HealthProgressInfo `json:"health,omitempty"`
	Sidecars             []SidecarInfo       `json:"sidecars,omitempty"` // containers only — e.g. dbgate, mongo-express
	RestartCount         int                 `json:"restart_count,omitempty"`
	ExternalRestartCount int                 `json:"external_restart_count,omitempty"`
	LastRestart          *GraphRestart       `json:"last_restart,omitempty"`
	StartupTime          string              `json:"startup_time,omitempty"`
	Uptime               string              `json:"uptime,omitempty"`
	// Kafka carries the produces/consumes declarations the node owns.
	// Services with no declared topics get nil (omitted from JSON);
	// externals are required by validation to declare at least one
	// topic, so the pointer is always non-nil for them.
	Kafka *config.KafkaIO `json:"kafka,omitempty"`
}

type GraphRestart struct {
	Source     string    `json:"source"`
	StartedAt  time.Time `json:"started_at"`
	ObservedAt time.Time `json:"observed_at"`
}

type GraphPortConflict struct {
	Port           int    `json:"port"`
	Resource       string `json:"resource"`
	PID            string `json:"pid,omitempty"`
	Process        string `json:"process,omitempty"`
	InspectCommand string `json:"inspect_command"`
}

// EdgeKind distinguishes startup-time synchronous dependencies (the
// existing model) from Kafka-mediated asynchronous producer/consumer
// relationships introduced by Service.Kafka and Externals.
type EdgeKind string

const (
	EdgeKindSync  EdgeKind = "sync"
	EdgeKindAsync EdgeKind = "async"
)

// GraphEdge is one directed edge in the graph.
type GraphEdge struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	Kind       EdgeKind `json:"kind"`
	Topic      string   `json:"topic,omitempty"`
	Detached   bool     `json:"detached"`
	Detachable bool     `json:"detachable"`
	EnvVars    []string `json:"env_vars,omitempty"`
}

func (s *Server) currentEnvName() string {
	if identity, managed := managedEnvironmentIdentity(s.ConfigPath()); managed {
		return identity
	}
	return EnvShortName(s.ConfigPath())
}

// statusesByName converts a []ResourceStatus slice into a name-keyed map.
func statusesByName(list []ResourceStatus) map[string]ResourceStatus {
	out := make(map[string]ResourceStatus, len(list))
	for i := range list {
		out[list[i].Name] = list[i]
	}
	return out
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	s.environmentTransitionMu.RLock()
	defer s.environmentTransitionMu.RUnlock()

	envName := s.currentEnvName()
	if envQuery := r.URL.Query().Get("env"); envQuery != "" && envQuery != envName {
		s.servePreviewGraph(w, envQuery)
		return
	}

	// One immutable snapshot for the whole request — status assembly and
	// graph builders must render the same config generation.
	cfg := s.holder.Load()
	statuses := statusesByName(s.computeStatuses(cfg))
	resp := GraphResponse{
		Env:    envName,
		Groups: buildGroupInfos(cfg),
		Nodes:  buildGraphNodes(cfg, statuses),
		Edges:  append(buildGraphEdges(cfg, s.settings, envName), buildAsyncEdges(cfg)...),
	}
	writeJSON(w, http.StatusOK, resp)
}

// servePreviewGraph returns the graph for another env from disk, with all
// node states forced to "pending". Read-only: never touches s.cfg or any
// orchestrator state — the daemon manages exactly one live env at a time
// and preview is a parallel read channel.
func (s *Server) servePreviewGraph(w http.ResponseWriter, envName string) {
	target, identity, workspace, err := resolveManagedSwitchTarget(envName, s.ConfigPath())
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Error: err.Error()})
		return
	}
	if _, err := os.Stat(target); err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Error: "env not found: " + envName})
		return
	}
	if _, managed := managedEnvironmentIdentity(target); !managed {
		identity = EnvShortName(target)
	}
	previousWorkspace, hadWorkspace := os.LookupEnv("WORKSPACE_ROOT")
	if workspace == "" {
		_ = os.Unsetenv("WORKSPACE_ROOT")
	} else {
		_ = os.Setenv("WORKSPACE_ROOT", workspace)
	}
	cfg, err := config.Load(target)
	restoreEnvironmentValue("WORKSPACE_ROOT", previousWorkspace, hadWorkspace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: "load: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, GraphResponse{
		Env:    identity,
		Groups: buildGroupInfos(cfg),
		Nodes:  buildGraphNodes(cfg, nil),
		Edges:  append(buildGraphEdges(cfg, s.settings, envName), buildAsyncEdges(cfg)...),
	})
}

// buildGroupInfos extracts group → services from cfg.Groups in deterministic
// order. Groups with no services are dropped (UI doesn't need empty
// clusters). Services listed under a group but not defined in cfg.Services
// are silently skipped — keeps yaml typos from breaking the graph.
func buildGroupInfos(cfg *config.Config) []GroupInfo {
	out := make([]GroupInfo, 0, len(cfg.Groups))
	for _, name := range sortedKeys(cfg.Groups) {
		g := cfg.Groups[name]
		svcs := make([]string, 0, len(g.Services))
		for _, s := range g.Services {
			if _, ok := cfg.Services[s]; ok {
				svcs = append(svcs, s)
			}
		}
		if len(svcs) == 0 {
			continue
		}
		out = append(out, GroupInfo{Name: name, Color: g.Color, Services: svcs})
	}
	return out
}

func buildGraphNodes(cfg *config.Config, statuses map[string]ResourceStatus) []GraphNode {
	nodes := make([]GraphNode, 0, len(cfg.Containers)+len(cfg.Services))
	for _, name := range sortedKeys(cfg.Containers) {
		c := cfg.Containers[name]
		n := GraphNode{
			Name:  name,
			Kind:  c.ResolveKind(),
			Icon:  infraIconForContainer(c),
			State: "pending",
		}
		if st, ok := statuses[name]; ok {
			n.State = st.State
			n.StateReason = resourceFailureSummary(st)
			n.FailureKind = st.FailureKind
			n.BlockedBy = st.BlockedBy
			n.PortConflict = graphPortConflict(st.PortConflict)
			n.LogsAvailable = st.LogsAvailable
			n.Ports = st.Ports
			n.URL = st.URL
			n.Health = st.HealthProgress
			n.Sidecars = st.Sidecars
			n.RestartCount = st.RestartCount
			n.ExternalRestartCount = st.ExternalRestartCount
			n.LastRestart = graphRestart(st.LastRestart)
			n.StartupTime = st.StartupTime
			n.Uptime = st.Uptime
		}
		nodes = append(nodes, n)
	}
	for _, name := range sortedKeys(cfg.Services) {
		svc := cfg.Services[name]
		n := GraphNode{Name: name, Kind: svc.ResolveKind(), State: "pending"}
		if len(svc.Kafka.Produces) > 0 || len(svc.Kafka.Consumes) > 0 {
			k := svc.Kafka
			n.Kafka = &k
		}
		if st, ok := statuses[name]; ok {
			n.State = st.State
			n.StateReason = resourceFailureSummary(st)
			n.FailureKind = st.FailureKind
			n.BlockedBy = st.BlockedBy
			n.PortConflict = graphPortConflictFor(name, statuses, nil)
			n.LogsAvailable = st.LogsAvailable
			n.Mode = st.Mode
			n.Ports = st.Ports
			n.URL = st.URL
			n.Health = st.HealthProgress
			n.RestartCount = st.RestartCount
			n.ExternalRestartCount = st.ExternalRestartCount
			n.LastRestart = graphRestart(st.LastRestart)
			n.StartupTime = st.StartupTime
			n.Uptime = st.Uptime
		}
		nodes = append(nodes, n)
	}
	for _, name := range sortedKeys(cfg.Externals) {
		ext := cfg.Externals[name]
		if ext == nil {
			continue
		}
		label := ext.Label
		if label == "" {
			label = name
		}
		k := ext.Kafka
		nodes = append(nodes, GraphNode{
			Name:  name,
			Kind:  "external",
			Label: label,
			Color: ext.Color,
			State: "pending",
			Kafka: &k,
		})
	}
	return nodes
}

func graphRestart(restart *ResourceRestart) *GraphRestart {
	if restart == nil {
		return nil
	}
	return &GraphRestart{
		Source: restart.Source, StartedAt: restart.StartedAt, ObservedAt: restart.ObservedAt,
	}
}

func graphPortConflictFor(
	name string,
	statuses map[string]ResourceStatus,
	seen map[string]bool,
) *GraphPortConflict {
	if seen == nil {
		seen = make(map[string]bool)
	}
	if seen[name] {
		return nil
	}
	seen[name] = true

	status, ok := statuses[name]
	if !ok {
		return nil
	}
	if status.PortConflict != nil {
		return graphPortConflict(status.PortConflict)
	}
	for _, dependency := range status.PendingDependencies {
		if conflict := graphPortConflictFor(dependency, statuses, seen); conflict != nil {
			return conflict
		}
	}
	return nil
}

func graphPortConflict(conflict *ResourcePortConflict) *GraphPortConflict {
	if conflict == nil {
		return nil
	}
	return &GraphPortConflict{
		Port:           conflict.Port,
		Resource:       conflict.Resource,
		PID:            conflict.PID,
		Process:        conflict.Process,
		InspectCommand: conflict.InspectCommand,
	}
}

func buildGraphEdges(cfg *config.Config, settings *Settings, envName string) []GraphEdge {
	edges := make([]GraphEdge, 0)
	addEdge := func(from, fromKind string, deps []string) {
		for _, dep := range deps {
			edge := GraphEdge{
				From:       from,
				To:         dep,
				Kind:       EdgeKindSync,
				Detached:   settings.IsEdgeDetached(envName, from, dep),
				Detachable: fromKind == "frontend",
			}
			edge.EnvVars = env.EnvVarsForDependency(dep, cfg)
			edges = append(edges, edge)
		}
	}
	for _, name := range sortedKeys(cfg.Containers) {
		c := cfg.Containers[name]
		addEdge(name, c.ResolveKind(), c.DependsOn)
	}
	for _, name := range sortedKeys(cfg.Services) {
		svc := cfg.Services[name]
		addEdge(name, svc.ResolveKind(), svc.DependsOn)
	}
	return edges
}

// buildAsyncEdges derives Kafka producer→consumer edges from the
// kafka.{produces,consumes} declarations on services and externals.
// One edge is emitted per (producer, consumer, topic) tuple. A node
// that both produces and consumes the same topic does not emit a
// self-loop — that case is surfaced only in NodeDrawer.
func buildAsyncEdges(cfg *config.Config) []GraphEdge {
	type ioSource struct {
		name string
		io   config.KafkaIO
	}
	var all []ioSource
	for _, name := range sortedKeys(cfg.Services) {
		svc := cfg.Services[name]
		if svc == nil {
			continue
		}
		all = append(all, ioSource{name: name, io: svc.Kafka})
	}
	for _, name := range sortedKeys(cfg.Externals) {
		ext := cfg.Externals[name]
		if ext == nil {
			continue
		}
		all = append(all, ioSource{name: name, io: ext.Kafka})
	}

	producers := map[string][]string{} // topic -> producer names
	consumers := map[string][]string{} // topic -> consumer names
	for _, s := range all {
		for _, t := range s.io.Produces {
			producers[t] = append(producers[t], s.name)
		}
		for _, t := range s.io.Consumes {
			consumers[t] = append(consumers[t], s.name)
		}
	}

	topicNames := make([]string, 0, len(producers))
	for t := range producers {
		topicNames = append(topicNames, t)
	}
	sort.Strings(topicNames)

	var edges []GraphEdge
	for _, topic := range topicNames {
		for _, p := range producers[topic] {
			for _, c := range consumers[topic] {
				if p == c {
					continue
				}
				edges = append(edges, GraphEdge{
					From:  p,
					To:    c,
					Kind:  EdgeKindAsync,
					Topic: topic,
				})
			}
		}
	}
	return edges
}

func (s *Server) handleEdgeDetach(w http.ResponseWriter, r *http.Request) {
	s.environmentTransitionMu.Lock()
	defer s.environmentTransitionMu.Unlock()
	if requireMethod(w, r, http.MethodPut) {
		return
	}
	// from/to come from the URL path only. The legacy body form
	// (/api/edges/detach with from/to in JSON) was removed after all clients
	// migrated to PUT /api/edges/{from}/{to}.
	rest := strings.TrimPrefix(r.URL.Path, "/api/edges/")
	from, to, ok := strings.Cut(rest, "/")
	if !ok || from == "" || to == "" || from == "detach" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "use PUT /api/edges/{from}/{to}"})
		return
	}

	var req EdgeDetachRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
		return
	}
	// Always use the server's current env — never trust the client-supplied
	// req.Env, which may be stale after a rapid env switch.
	envName := s.currentEnvName()
	if envName == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "no env loaded"})
		return
	}

	svc, exists := s.holder.Load().Services[from]
	if !exists || svc.ResolveKind() != "frontend" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "only frontend services can detach dependencies"})
		return
	}

	if err := s.settings.SetEdgeDetached(envName, from, to, req.Detached); err != nil {
		slog.Error("persist detached edge", "component", "graph", "err", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	// Propagate the change to the running orchestrator so calcPendingDeps
	// picks up the new state immediately — without this the orchestrator
	// held a stale snapshot until the next `orbit up`.
	s.app.Orchestrator.UpdateDetachedDeps(s.settings.GetDetachedEdges(envName))

	action := "attached"
	if req.Detached {
		action = "detached"
	}
	writeJSON(w, http.StatusOK, APIResponse{
		OK:      true,
		Message: from + "→" + to + " " + action + ". Takes effect on next `orbit up` or restart.",
	})
}
