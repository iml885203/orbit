package tunnel

import (
	"context"
	"net/http"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
)

// SetupDaemon wires tunnel routes, resources, events, and shutdown cleanup
// before the daemon listener starts.
func SetupDaemon(host extension.Host, mux *http.ServeMux) extension.DaemonHooks {
	hooks := extension.DaemonHooks{}

	hub := NewAccessLogHub(200)
	tm := NewTunnelManager(host, hub)
	feat := NewTunnelFeature(tm)

	mux.HandleFunc("/api/tunnel", feat.HandleTunnel)
	mux.HandleFunc("/api/tunnel/", feat.HandleTunnel)

	// Resource contribution is a daemon-typed capability (the snapshot
	// shape shares HealthProgressInfo with /api/status), reached by
	// asserting the host — see daemon.ResourceRegistrar.
	if rr, ok := host.(daemon.ResourceRegistrar); ok {
		rr.AddResourceContributor(func(context.Context) []daemon.ResourceSnapshot {
			return SnapshotTunnels(tm.Views())
		})
	}

	hooks.EventSources = append(hooks.EventSources,
		extension.EventSource{Name: "tunnel-access", Run: extension.RunChannel(hub.Subscribe)})
	// Release claims before daemon teardown so the gateway drops the
	// leases immediately instead of waiting for lease expiry.
	hooks.OnDown = append(hooks.OnDown, tm.ReleaseAllOnShutdown)
	return hooks
}
