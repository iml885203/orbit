package daemon

import (
	"context"
	"net"
	"net/http"
	"time"
)

// DoctorCheckStatus represents the result of a health check.
type DoctorCheckStatus string

const (
	CheckPass DoctorCheckStatus = "pass"
	CheckFail DoctorCheckStatus = "fail"
	CheckWarn DoctorCheckStatus = "warn"
	CheckInfo DoctorCheckStatus = "info"
)

// DoctorCheck is a single health check result.
type DoctorCheck struct {
	Name    string            `json:"name"`
	Status  DoctorCheckStatus `json:"status"`
	Message string            `json:"message"`
	Hint    string            `json:"hint,omitempty"`
}

// DoctorResponse is the response for GET /api/doctor.
type DoctorResponse struct {
	Checks []DoctorCheck `json:"checks"`
	RanAt  string        `json:"ran_at"`
}

// TraceLogLine is one log line that belongs to a trace, with the service it
// came from so merged views stay attributable.
type TraceLogLine struct {
	Service string `json:"service"`
	Line    string `json:"line"`
}

// TraceLogsResponse is the response for GET /api/traces/{id}/logs.
type TraceLogsResponse struct {
	Lines []TraceLogLine `json:"lines"`
}

// EnvInfo describes one available env config file.
type EnvInfo struct {
	Identity string `json:"identity"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Selected bool   `json:"selected"`
	Running  bool   `json:"running"`
}

type EnvironmentSourceInfo struct {
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	Location      string    `json:"location"`
	Workspace     string    `json:"workspace,omitempty"`
	Default       bool      `json:"default"`
	Ref           string    `json:"ref,omitempty"`
	ResolvedRef   string    `json:"resolved_ref,omitempty"`
	Commit        string    `json:"commit,omitempty"`
	LastSyncError string    `json:"last_sync_error,omitempty"`
	Environments  []EnvInfo `json:"environments"`
}

// EnvsResponse is returned by GET /api/envs.
type EnvsResponse struct {
	Current string                  `json:"current,omitempty"`
	Running int                     `json:"running"` // count of non-stopped services
	Sources []EnvironmentSourceInfo `json:"sources"`
	Context EnvironmentContext      `json:"context"`
}

type EnvironmentSwitchResponse struct {
	OK                   bool                `json:"ok,omitempty"`
	Error                string              `json:"error,omitempty"`
	Message              string              `json:"message,omitempty"`
	ConfirmationRequired bool                `json:"confirmation_required,omitempty"`
	CurrentContext       *EnvironmentContext `json:"current_context,omitempty"`
	TargetContext        *EnvironmentContext `json:"target_context,omitempty"`
	RunningResources     []string            `json:"running_resources,omitempty"`
}

type EnvironmentReconcileResponse struct {
	OK                   bool     `json:"ok,omitempty"`
	Error                string   `json:"error,omitempty"`
	RestartRequired      bool     `json:"restart_required,omitempty"`
	PreviouslyRunning    []string `json:"previously_running"`
	RestartedResources   []string `json:"restarted_resources"`
	StartedDependencies  []string `json:"started_dependencies"`
	UnavailableResources []string `json:"unavailable_resources"`
	AffectedResources    []string `json:"affected_resources"`
}

// VersionResponse reports the running daemon's build and a binary Orbit can
// order after it. Older or incomparable builds are omitted so
// clients never recommend a restart that could downgrade the user.
type VersionResponse struct {
	Running         string `json:"running"`
	OnDisk          string `json:"on_disk,omitempty"`
	OnDiskPath      string `json:"on_disk_path,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
}

// SocketHTTPClient creates an http.Client that dials a unix socket.
func SocketHTTPClient(socketPath string) *http.Client {
	return SocketHTTPClientWithTimeout(socketPath, 5*time.Second, 2*time.Minute)
}

// SocketHTTPClientWithTimeout creates an http.Client for low-latency internal
// calls where the default long operation timeout would be too expensive.
func SocketHTTPClientWithTimeout(socketPath string, dialTimeout, requestTimeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: dialTimeout}
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: requestTimeout,
	}
}

// EnvToggleUpdateRequest is the PUT body.
type EnvToggleUpdateRequest struct {
	Service string `json:"service"`
	Var     string `json:"var"`
	Enabled bool   `json:"enabled"`
}

// EdgeDetachRequest is the PUT body for /api/edges/{from}/{to}.
type EdgeDetachRequest struct {
	// Env is accepted for back-compat but ignored server-side; the server
	// derives the env from its currently loaded config (currentEnvName()).
	// Clients SHOULD continue sending it so old daemons keep working.
	Env      string `json:"env"`
	Detached bool   `json:"detached"`
}

// ServiceModeRequest is the body for PUT /api/service-mode/{name}.
type ServiceModeRequest struct {
	Mode string `json:"mode"` // "dev" or "container"
}

// SettingsResponse is the /api/settings response. Fields duplicate APIResponse
// rather than embed it so the wire shape stays flat for TS codegen.
type SettingsResponse struct {
	OK      bool   `json:"ok,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ResourceSchemaVersion versions the snapshot wire shape. Additive changes
// (new fields, new resource types, new state values) do not bump it —
// consumers must tolerate unknown values; renames and removals do.
const ResourceSchemaVersion = 1

// ResourceSnapshot is the uniform, type-agnostic view of one thing orbit
// knows about: a container, a service, an external system, a database, a
// rebuild job, a tunnel, a claimed route. Consumers render name / type /
// state / properties generically without knowing concrete resource kinds —
// the design that lets an extension surface its own resources without
// changing every renderer. This endpoint coexists with the purpose-built
// /api/status and /api/graph views; it does not replace them.
type ResourceSnapshot struct {
	Name string `json:"name"`
	// Type is an open vocabulary: container, service, external, database,
	// job, tunnel, route today. Consumers must render unknown types
	// generically rather than reject them.
	Type        string `json:"type"`
	State       string `json:"state,omitempty"`
	StateReason string `json:"state_reason,omitempty"`
	FailureKind string `json:"failure_kind,omitempty"`
	BlockedBy   string `json:"blocked_by,omitempty"`
	// Parent groups a resource under another (database → sql-server,
	// route → tunnel). Empty for top-level resources.
	Parent    string   `json:"parent,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	URLs      []string `json:"urls,omitempty"`
	// Properties are display-oriented facts. Flat k/v on purpose — nested
	// payloads belong in purpose-built endpoints; what doesn't fit here is
	// the measured limit of generic rendering. Keys are opaque identifiers:
	// consumers must not parse them (the "port:redis" colon is a naming
	// convention, not a protocol).
	Properties map[string]string `json:"properties,omitempty"`
	// Health is the one sanctioned nested exception to the flat contract:
	// it is shared verbatim with /api/status, and its Recovering flag is
	// load-bearing for wait semantics — flattening would fork the shape.
	Health *HealthProgressInfo `json:"health,omitempty"`
}

// ResourcesResponse is the GET /api/resources payload.
type ResourcesResponse struct {
	SchemaVersion int                `json:"schema_version"`
	Env           string             `json:"env"`
	Resources     []ResourceSnapshot `json:"resources"`
}
