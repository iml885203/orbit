package health

import (
	"fmt"
	"sync"
)

// Progress carries the current retry state of a service's health check.
// Zero value (Configured=false) means the service has no health check
// configured, so callers should hide retry counters in their UI.
type Progress struct {
	Configured bool   // false if service has no health_check stanza
	Attempts   int    // total checks performed since last reset
	MaxRetries int    // configured retry budget (0 if unbounded)
	LastErr    string // empty when last attempt succeeded
	// Recovering means the startup retry budget is spent but recovery
	// probing is still running — Attempts stays pinned at MaxRetries while
	// LastErr keeps updating. Consumers (CLI waits, dashboard) treat a
	// degraded-but-recovering service as "still trying", not terminal.
	Recovering bool
	// recoveringOwner is the service generation that set Recovering. Clears
	// are owner-scoped so a cancelled loop from a previous start can't drop
	// the flag its successor just raised.
	recoveringOwner int
}

// progressTracker is embedded in Checker. Kept in a separate file so the
// retry tracking concern is isolated from the probe-strategy code.
type progressTracker struct {
	mu sync.RWMutex
	m  map[string]Progress
}

func newProgressTracker() *progressTracker {
	return &progressTracker{m: make(map[string]Progress)}
}

func (t *progressTracker) get(name string) Progress {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.m[name]
}

func (t *progressTracker) record(name string, configured bool, attempts, maxRetries int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := Progress{
		Configured: configured,
		Attempts:   attempts,
		MaxRetries: maxRetries,
	}
	if err != nil {
		p.LastErr = err.Error()
	}
	t.m[name] = p
}

// recordRecovering marks name as in recovery probing for owner, refreshing
// LastErr while leaving the exhausted startup counters in place.
func (t *progressTracker) recordRecovering(name string, owner int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.m[name]
	p.Configured = true
	p.Recovering = true
	p.recoveringOwner = owner
	if err != nil {
		p.LastErr = err.Error()
	}
	t.m[name] = p
}

// clearRecovering drops the recovery flag without touching counters —
// called when a recovery loop exits without success (cancelled by stop/
// restart or vetoed) so a ghost Recovering can't make CLI waits treat a
// truly terminal degraded as still-trying. Owner-scoped: a loop only
// clears the flag it set, never a successor generation's.
func (t *progressTracker) clearRecovering(name string, owner int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.m[name]
	if !ok || p.recoveringOwner != owner {
		return
	}
	p.Recovering = false
	t.m[name] = p
}

func (t *progressTracker) reset(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, name)
}

// Progress returns a snapshot of name's health-check progress.
// Returns the zero value if name is unknown — callers treat Configured=false
// as "no progress to show."
func (c *Checker) Progress(name string) Progress {
	return c.progress.get(name)
}

func (c *Checker) recordProgress(name string, configured bool, attempts, maxRetries int, err error) {
	c.progress.record(name, configured, attempts, maxRetries, err)
}

// MarkRecovering flags name as in recovery probing without touching the
// counters or LastErr. Exported so the engine can set it BEFORE emitting the
// health-fail event that flips the service to degraded — CLI waits read
// degraded+recovering as non-terminal, so the flag must never lag the state.
// owner is the service generation; clears are scoped to it.
func (c *Checker) MarkRecovering(name string, owner int) {
	c.progress.recordRecovering(name, owner, nil)
}

func (c *Checker) recordRecovering(name string, owner int, err error) {
	c.progress.recordRecovering(name, owner, err)
}

func (c *Checker) clearRecovering(name string, owner int) {
	c.progress.clearRecovering(name, owner)
}

func (c *Checker) resetProgress(name string) {
	c.progress.reset(name)
}

// RecordProgressForTest is exposed for daemon-level integration tests that
// need to inject health progress without running a real retry loop.
// Production code uses recordProgress directly inside pollWithProbe.
func (c *Checker) RecordProgressForTest(name string, attempts, maxRetries int, lastErr string) {
	var err error
	if lastErr != "" {
		err = fmt.Errorf("%s", lastErr)
	}
	c.recordProgress(name, true, attempts, maxRetries, err)
}
