// Package daemon is the public wire-and-host surface of the orbit
// daemon: the JSON contracts the CLI, dashboard, and extensions speak,
// the socket client, settings, and the capability types a feature's
// DaemonSetup consumes. The concrete HTTP server lives in the
// core-private internal/daemon — it implements these contracts; nothing
// here depends on it.
package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// UpRequest is the body for POST /api/up.
type UpRequest struct {
	Resources []string `json:"resources"`
	InfraOnly bool     `json:"infra_only"`
	Groups    []string `json:"groups"`
}

// DownRequest is the body for POST /api/down.
type DownRequest struct {
	All  bool `json:"all"`
	Wait bool `json:"wait"`
}

// StatusResponse is the response for GET /api/status.
type StatusResponse struct {
	// Epoch is the daemon's startedAt timestamp in unix-milliseconds. It
	// changes on every restart, so clients can detect when their cached
	// env-derived state belongs to a previous daemon. Milliseconds (not
	// nanoseconds) keeps the value safely within JS Number precision.
	Epoch     int64           `json:"epoch"`
	Resources []ServiceStatus `json:"resources"`
	// ConfigPath identifies the environment loaded by the running daemon.
	// CLI clients compare it with their selected config before combining
	// local config with daemon state.
	ConfigPath string `json:"config_path"`
	// ConfigStale means the loaded config has fallen behind reality (env
	// file edited, selection changed, or an API env switch left the
	// orchestrator on the previous env) — `orbit daemon restart` applies.
	ConfigStale       bool   `json:"config_stale,omitempty"`
	ConfigStaleReason string `json:"config_stale_reason,omitempty"`
}

// ServiceKind narrows the set of service kinds the wire API exposes.
type ServiceKind string

const (
	ServiceKindContainer ServiceKind = "container"
	ServiceKindService   ServiceKind = "service"
)

// SidecarInfo represents a sidecar's UI link.
type SidecarInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ServiceStatus represents a single service's status.
type ServiceStatus struct {
	Name  string      `json:"name"`
	Kind  ServiceKind `json:"kind"`
	State string      `json:"state"`
	// PendingDependencies identifies exactly what keeps a pending service
	// from starting, so clients can distinguish useful waiting from a
	// terminal dependency failure.
	PendingDependencies []string `json:"pending_dependencies,omitempty"`
	// StateReason says why the service is degraded (crash message, health
	// failure, build failure); empty in every other state.
	StateReason    string              `json:"state_reason,omitempty"`
	RestartCount   int                 `json:"restart_count"`
	Ports          map[string]int      `json:"ports,omitempty"`
	URL            string              `json:"url,omitempty"`
	Image          string              `json:"image,omitempty"` // resolved container image; containers only
	StartupTime    string              `json:"startup_time,omitempty"`
	Uptime         string              `json:"uptime,omitempty"`
	Sidecars       []SidecarInfo       `json:"sidecars,omitempty"`
	Mode           string              `json:"mode,omitempty"` // "dev" or "container" (only for dual-defined services)
	HealthProgress *HealthProgressInfo `json:"health_progress,omitempty"`
}

// HealthProgressInfo reports retry state for a service that has a
// configured health check. nil on ServiceStatus means the service either
// has no health check or has not yet attempted one (caller hides UI).
type HealthProgressInfo struct {
	Attempts   int    `json:"attempts"`
	MaxRetries int    `json:"max_retries,omitempty"`
	LastErr    string `json:"last_err,omitempty"`
	// Recovering means the startup retry budget is spent but the daemon is
	// still probing and will flip the service back to healthy on success.
	// CLI waits treat degraded+recovering as "still trying", not terminal.
	Recovering bool `json:"recovering,omitempty"`
}

// LogsResponse is the response for GET /api/logs/{name}.
type LogsResponse struct {
	Lines []string `json:"lines"`
}

// APIResponse is a generic API response wrapper.
type APIResponse struct {
	OK      bool   `json:"ok,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	// AffectedResources identifies the resources accepted by a lifecycle
	// request, including dependencies selected by the daemon.
	AffectedResources []string `json:"affected_resources,omitempty"`
	// Code is a stable, machine-readable discriminator a client can
	// switch on when the plain Error string isn't enough — e.g. the
	// reset endpoint's "reset_requires_recreate". Empty for most errors.
	Code string `json:"code,omitempty"`
}

// WriteJSON writes v as JSON with the given status — the daemon's one
// response serializer, shared by core handlers and extension-owned ones.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("json encode error", "component", "daemon", "err", err)
	}
}

// RequireMethod checks the HTTP method and writes a 405 response if it
// doesn't match. Returns true when the method is wrong (caller returns).
func RequireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		WriteJSON(w, http.StatusMethodNotAllowed, APIResponse{Error: "method not allowed"})
		return true
	}
	return false
}
