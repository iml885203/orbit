package app

import (
	"fmt"
	"sort"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

type relocatedPortJSON struct {
	Resource  string `json:"resource"`
	Label     string `json:"label"`
	Preferred int    `json:"preferred"`
	Actual    int    `json:"actual"`
}

// relocatedPorts compares declared preferences against the observed
// selections so consumers outside the stack (DB clients, scripts,
// bookmarks) learn a move without diffing status by hand. Deliberately
// re-derived from cfg+status instead of carried on the wire: the daemon
// would otherwise have to persist per-resource relocation events across
// restarts, while declared-vs-observed is stateless and reports the truth
// whether the move came from boot-time resolution, a stale-port refresh,
// or an earlier daemon generation.
func relocatedPorts(cfg *config.Config, status *daemon.StatusResponse) []relocatedPortJSON {
	if cfg == nil || status == nil {
		return nil
	}
	declared := map[string]map[string]config.PortDef{}
	for name, container := range cfg.Containers {
		declared[name] = container.Ports
	}
	for name, service := range cfg.Services {
		declared[name] = service.Ports
	}
	var moves []relocatedPortJSON
	for i := range status.Resources {
		resource := &status.Resources[i]
		for label, def := range declared[resource.Name] {
			if !def.IsAuto() {
				continue
			}
			observed := resource.Ports[label]
			if observed != 0 && observed != def.PreferredHost() {
				moves = append(moves, relocatedPortJSON{
					Resource:  resource.Name,
					Label:     label,
					Preferred: def.PreferredHost(),
					Actual:    observed,
				})
			}
		}
	}
	sort.Slice(moves, func(i, j int) bool {
		if moves[i].Resource != moves[j].Resource {
			return moves[i].Resource < moves[j].Resource
		}
		return moves[i].Label < moves[j].Label
	})
	return moves
}

func printRelocatedPorts(cfg *config.Config, status *daemon.StatusResponse) {
	for _, move := range relocatedPorts(cfg, status) {
		fmt.Printf("  → %s %s: preferred port %d was taken, using %d\n",
			move.Resource, move.Label, move.Preferred, move.Actual)
	}
}
