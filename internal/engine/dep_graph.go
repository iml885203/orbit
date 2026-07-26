package engine

import "github.com/iml885203/orbit/config"

// DepGraph is a snapshot of the dependency adjacency for a Config,
// with detached edges already filtered out. It's the single source
// of truth for "who depends on what" inside the engine — BuildDAG,
// Orchestrator.calcPendingDeps, and scheduler.AddDepsWithDetached
// all read from one.
type DepGraph struct {
	// deps maps a node to the list of nodes it depends on (after detach filter).
	deps map[string][]string
	// nodes is the full set of known node names (including containers
	// without deps, so callers can iterate without re-reading cfg).
	nodes map[string]struct{}
}

// NewDepGraph builds a DepGraph from cfg and the detached overlay.
// detached is keyed by "from" service name; values are the list of
// "to" deps to skip. Pass nil for no filtering.
func NewDepGraph(cfg *config.Config, detached map[string][]string) *DepGraph {
	g := &DepGraph{
		deps:  make(map[string][]string, len(cfg.Services)+len(cfg.Containers)),
		nodes: make(map[string]struct{}, len(cfg.Services)+len(cfg.Containers)),
	}
	for name, c := range cfg.Containers {
		g.nodes[name] = struct{}{}
		g.deps[name] = filterList(c.DependsOn, detached[name])
	}
	for name, s := range cfg.Services {
		g.nodes[name] = struct{}{}
		g.deps[name] = filterList(s.DependsOn, detached[name])
	}
	return g
}

// DepsOf returns the (filtered) dependency list for a node. Returns
// nil if the node is unknown.
func (g *DepGraph) DepsOf(name string) []string { return g.deps[name] }

// Nodes returns the full set of known node names.
func (g *DepGraph) Nodes() map[string]struct{} { return g.nodes }

// AllDeps returns a copy of the full deps map. Used by BuildDAG which
// returns it as part of its public API.
func (g *DepGraph) AllDeps() map[string][]string {
	out := make(map[string][]string, len(g.deps))
	for k, v := range g.deps {
		out[k] = v
	}
	return out
}

// filterList removes any entry in list that appears in skipList.
// Returns the original slice when skipList is empty.
func filterList(list, skipList []string) []string {
	if len(skipList) == 0 {
		return list
	}
	skip := make(map[string]bool, len(skipList))
	for _, s := range skipList {
		skip[s] = true
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if !skip[item] {
			out = append(out, item)
		}
	}
	return out
}
