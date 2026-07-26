package devdb

// Server-side memory of each database's last schema diff, so the
// dashboard's drift badges survive a page reload instead of living only
// in one page's Svelte state. In-memory only: a daemon restart clears it
// and rows simply show as not-yet-checked. Entries go stale — not away —
// when a publish/reset lands for the database, mirroring the dashboard's
// own semantics (show last-known drift, flagged as out of date).

import (
	"sort"
	"sync"
	"time"

	"github.com/iml885203/orbit/internal/sqlpublish"
)

// DriftEntry is one database's cached diff outcome: either a structured
// Result or a failure (Error + Code) — never both. At is the RFC3339
// time the diff ran; Stale marks a publish/reset since then.
type DriftEntry struct {
	DB     string                 `json:"db"`
	Result *sqlpublish.DiffResult `json:"result,omitempty"`
	Error  string                 `json:"error,omitempty"`
	Code   string                 `json:"code,omitempty"`
	At     string                 `json:"at"`
	Stale  bool                   `json:"stale,omitempty"`
}

// DriftSnapshotResponse is the body of GET /api/db/drift: every cached
// entry in stable (by DB name) order.
type DriftSnapshotResponse struct {
	Entries []DriftEntry `json:"entries"`
}

// driftCache guards the per-DB entries; all access goes through its
// methods, which own the lock.
type driftCache struct {
	mu           sync.Mutex
	entries      map[string]DriftEntry
	generation   uint64
	dbGeneration map[string]uint64
}

func newDriftCache() *driftCache {
	return &driftCache{
		entries:      map[string]DriftEntry{},
		dbGeneration: map[string]uint64{},
	}
}

type driftGeneration struct {
	all uint64
	db  uint64
}

func (c *driftCache) currentGeneration(db string) driftGeneration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return driftGeneration{all: c.generation, db: c.dbGeneration[db]}
}

func (c *driftCache) recordIfCurrent(db string, generation driftGeneration, result *sqlpublish.DiffResult, code sqlpublish.ErrorCode, err error) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation.all != c.generation || generation.db != c.dbGeneration[db] {
		return false
	}
	c.recordLocked(db, result, code, err)
	return true
}

func (c *driftCache) recordLocked(db string, result *sqlpublish.DiffResult, code sqlpublish.ErrorCode, err error) {
	e := DriftEntry{DB: db, At: time.Now().Format(time.RFC3339)}
	if err != nil {
		e.Error = err.Error()
		e.Code = string(code)
	} else {
		e.Result = result
	}
	c.entries[db] = e
}

// markStale flags db's entry as out of date after its publish/reset.
// No-op when the DB was never diffed.
func (c *driftCache) markStale(db string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dbGeneration[db]++
	e, ok := c.entries[db]
	if !ok {
		return
	}
	e.Stale = true
	c.entries[db] = e
}

func (c *driftCache) markAllStale() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	for db, entry := range c.entries {
		entry.Stale = true
		c.entries[db] = entry
	}
}

// snapshot returns every entry in stable (by DB name) order.
func (c *driftCache) snapshot() []DriftEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]DriftEntry, 0, len(c.entries))
	for _, e := range c.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DB < out[j].DB })
	return out
}
