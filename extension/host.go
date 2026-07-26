package extension

import (
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/process"
)

// Host is the daemon surface an extension's DaemonSetup receives. Narrow
// by design: only what registered features actually consume, growing
// per-seam with the extraction batches. Daemon-typed capabilities beyond
// this contract (e.g. resource-snapshot contribution) are reached by
// type-asserting the host to the daemon-side interface — the extension
// package stays free of daemon imports (the dependency points the other
// way).
type Host interface {
	// Config returns the current published snapshot — one Load per
	// operation, never cached across operations.
	Config() *config.Config
	// UpdateConfig runs fn under the daemon's config writer lock (see
	// ConfigTx for the critical-section surface).
	UpdateConfig(fn func(tx ConfigTx) error) error
	// ProcessMgr is the shared child-process manager for dev-service
	// processes. (The tunnel feature no longer spawns children — the
	// Tunlease client is an in-process library.) Implementations must
	// return the same instance on every call — features spawn through it
	// at different times and rely on one manager owning all children.
	ProcessMgr() *process.Manager
}

// ConfigTx is the surface a config writer sees inside Host.UpdateConfig's
// critical section. Both daemon writer shapes are expressible without
// touching the lock itself:
//
//   - splice: mutate process env, re-Load from the current config path,
//     then Store(Current().WithContainer(...)) — config.Load's output
//     depends on process env, so the re-Load must happen under the lock;
//   - env switch: Load the target file, Store the whole config, then
//     SetConfigPath + MarkEngineStale — the metadata updates stay inside
//     the lock because a racing splice reads the config path in its own
//     critical section; moving them outside lets it Load the OLD path
//     against the NEW config (lost update).
//
// Store publishes an immutable snapshot (see config.Holder): never mutate
// a config after storing it, splice via the With* copy helpers instead.
type ConfigTx interface {
	// Current returns the currently published snapshot.
	Current() *config.Config
	// Load reads and validates a config file under the writer lock.
	Load(path string) (*config.Config, error)
	// Store publishes cfg to every reader. It performs no validation:
	// cfg must be non-nil and already validated — a tx.Load result or a
	// With* splice of Current(). Publishing anything else poisons every
	// reader in the process.
	Store(cfg *config.Config)
	// SetConfigPath moves the daemon's config path (and staleness
	// baseline) to path.
	SetConfigPath(path string)
	// MarkEngineStale flags that the orchestrator's service graph was
	// built from a config that is no longer published (sticky until the
	// daemon restarts).
	MarkEngineStale()
}
