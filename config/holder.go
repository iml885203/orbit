package config

import "sync/atomic"

// Holder publishes an immutable *Config via atomic swap. The daemon and
// the engine share one Holder, so every reader sees the same generation
// of config without locks.
//
// Contract: a Config handed to Store MUST NOT be mutated afterwards at
// ANY level — its maps and slices may be aliased by downstream holders
// (DepGraph keeps DependsOn slices, BuildEnv walks Containers), so
// mutation after Store corrupts readers with no lock to save them.
// Build a replacement (config.Load or Config.WithContainer) instead.
//
// Writers must serialize among themselves (the daemon's configWriteMu):
// Load→build→Store is a read-modify-write, and two unserialized writers
// lose one of the updates.
type Holder struct {
	p atomic.Pointer[Config]
}

// NewHolder publishes initial. initial must be non-nil — Load never
// returns nil afterwards.
func NewHolder(initial *Config) *Holder {
	h := &Holder{}
	h.p.Store(initial)
	return h
}

// Load returns the current snapshot. Callers take one snapshot per
// operation (one HTTP request, one event-loop iteration) and read only
// that snapshot for self-consistency.
func (h *Holder) Load() *Config {
	return h.p.Load()
}

// Store publishes a new immutable snapshot.
func (h *Holder) Store(c *Config) {
	h.p.Store(c)
}
