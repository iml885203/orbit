package daemon

import (
	"context"
	"net/http"
)

// Capability contracts a feature's DaemonSetup asserts on its host —
// implemented by the core-private server. Kept here so extension code
// depends only on this public package.

// ResourceContributor adds feature-owned entries to GET /api/resources.
// Daemon-typed on purpose (ResourceSnapshot carries HealthProgressInfo,
// shared verbatim with /api/status) — extensions reach it by asserting
// their Host to ResourceRegistrar instead of the contract living in the
// extension package.
type ResourceContributor func(ctx context.Context) []ResourceSnapshot

// ResourceRegistrar is the daemon-side capability DaemonSetup asserts to
// contribute resources: `host.(daemon.ResourceRegistrar)`.
type ResourceRegistrar interface {
	AddResourceContributor(ResourceContributor)
}

// SettingsChange describes one flat settings key mutated by a PUT — Old
// captured before the write so hooks can react to transitions (the SQL
// mode switch compares old vs new image).
type SettingsChange struct {
	Key, Old, New string
}

// SettingsPUTHook runs inside PUT /api/settings after the generic fields
// persist. A hook that writes the HTTP response returns true; otherwise
// the generic "settings saved" reply follows.
type SettingsPUTHook func(w http.ResponseWriter, changes []SettingsChange) (handled bool)

// SettingsHookRegistrar is the daemon-side capability DaemonSetup
// asserts to intercept settings PUTs: `host.(daemon.SettingsHookRegistrar)`.
type SettingsHookRegistrar interface {
	AddSettingsPUTHook(SettingsPUTHook)
}

// ContainerOps is the narrow container surface features consume.
type ContainerOps interface {
	Stop(ctx context.Context, name string) error
	ImageExists(image string) bool
	CheckImagePull(ctx context.Context, image string) error
}

// ServiceRestarter is the narrow orchestrator surface features consume.
type ServiceRestarter interface {
	RestartService(ctx context.Context, name string) error
}

// DoctorRegistrar is the daemon-side capability DaemonSetup asserts to
// contribute doctor check groups (the DB workflow's checks).
type DoctorRegistrar interface {
	AddDoctorChecks(func() []DoctorCheck)
}
