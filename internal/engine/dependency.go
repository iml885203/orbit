package engine

import "github.com/iml885203/orbit/config"

// BuildDAGWithDetached returns a topologically sorted start order and the
// full deps adjacency, honoring a detached-edges overlay.
func BuildDAGWithDetached(cfg *config.Config, detached map[string][]string) ([]string, map[string][]string) {
	g := NewDepGraph(cfg, detached)
	return topoSort(g), g.AllDeps()
}

func topoSort(g *DepGraph) []string {
	inDegree := make(map[string]int, len(g.nodes))
	reverse := make(map[string][]string)
	for name := range g.nodes {
		inDegree[name] = 0
	}
	for name := range g.nodes {
		deps := g.DepsOf(name)
		inDegree[name] = len(deps)
		for _, dep := range deps {
			reverse[dep] = append(reverse[dep], name)
		}
	}

	queue := make([]string, 0)
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}

	order := make([]string, 0, len(g.nodes))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		order = append(order, node)
		for _, dependent := range reverse[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}
	return order
}
