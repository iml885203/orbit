package dbstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/iml885203/orbit/atomicio"
)

// Store maintains per-db state in memory and on disk. Daemon-owned;
// CLI mutates via POST /api/db-state/event. All exported methods are
// safe for concurrent use.
//
// Subscribe coalesces stale snapshots when a subscriber is slow (buffer=1,
// drain-then-push). Subscribers are never dropped, so terminal state always
// reaches the client. See Subscribe and broadcastLocked for details.
type Store struct {
	mu   sync.Mutex // guards dbs and subs
	path string
	dbs  map[string]DBState
	subs map[chan Snapshot]struct{}
}

// New opens (or creates) the store backed by <dir>/db-state.json.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	s := &Store{
		path: filepath.Join(dir, "db-state.json"),
		dbs:  map[string]DBState{},
		subs: map[chan Snapshot]struct{}{},
	}
	s.load()
	return s, nil
}

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return // first start: empty state
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return // malformed file ignored; degrade to empty
	}
	if snap.DBs != nil {
		s.dbs = snap.DBs
	}
}

func (s *Store) snapshotLocked() Snapshot {
	out := make(map[string]DBState, len(s.dbs))
	for k, v := range s.dbs {
		out[k] = v
	}
	return Snapshot{
		Version:   fileVersion,
		UpdatedAt: time.Now(),
		DBs:       out,
	}
}

// Snapshot returns a deep copy of the current state, safe to send over
// the wire.
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Store) persistLocked() error {
	snap := s.snapshotLocked()
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return atomicio.WriteFile(s.path, data, 0644)
}

// broadcastLocked must be called with s.mu held. Coalesces stale snapshots
// in slow subscribers instead of dropping them — see Subscribe.
func (s *Store) broadcastLocked() {
	snap := s.snapshotLocked()
	for ch := range s.subs {
		select {
		case <-ch:
		default:
		}
		ch <- snap
	}
}

// Subscribe returns a channel that receives a full Snapshot on every change.
// The initial buffered value is the current state. Caller MUST invoke the
// returned cancel func when done.
//
// Drop policy: buffer is 1; on overflow broadcastLocked drains the stale
// snapshot and replaces it with the latest. Since each frame is a full
// snapshot, coalescing during slow-consumer stalls is correct — the latest
// value supersedes any pending one, and the subscriber is never dropped, so
// terminal state always reaches the client. See go-event-loop.md.
func (s *Store) Subscribe() (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, 1)
	s.mu.Lock()
	ch <- s.snapshotLocked() // initial frame
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		s.mu.Unlock()
	}
}

func (s *Store) commitLocked() error {
	if err := s.persistLocked(); err != nil {
		return err
	}
	s.broadcastLocked()
	return nil
}

// Publish records a successful schema publish on db (the generic
// host-side publish path). Other fields untouched.
func (s *Store) Publish(db string, source Source, durationMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.dbs[db]
	cur.Name = db
	cur.LastPublish = &Event{At: time.Now(), Source: source, DurationMs: durationMs}
	s.dbs[db] = cur
	return s.commitLocked()
}

// PublishClean records a successful clean publish: LastPublish and the
// refreshed baseline both move.
func (s *Store) PublishClean(db string, source Source, durationMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.dbs[db]
	cur.Name = db
	now := time.Now()
	cur.LastPublish = &Event{At: now, Source: source, DurationMs: durationMs}
	declareBaseline(&cur, now, source)
	s.dbs[db] = cur
	return s.commitLocked()
}

// SnapshotRefreshed records an explicit baseline refresh.
func (s *Store) SnapshotRefreshed(db string, source Source) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.dbs[db]
	cur.Name = db
	declareBaseline(&cur, time.Now(), source)
	s.dbs[db] = cur
	return s.commitLocked()
}

// declareBaseline marks the database's CURRENT contents as its clean
// baseline. Single owner of the invariant: declaring a baseline absorbs
// any recorded delta above the old one (mirrors Reset semantics), so
// LastApply clears together with the timestamp move.
func declareBaseline(cur *DBState, at time.Time, source Source) {
	cur.BaselineAt = &Event{At: at, Source: source}
	cur.LastApply = nil
}

// Apply records a successful apply on db. Other fields untouched.
func (s *Store) Apply(db string, source Source, durationMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.dbs[db]
	cur.Name = db
	cur.LastApply = &Event{At: time.Now(), Source: source, DurationMs: durationMs}
	s.dbs[db] = cur
	return s.commitLocked()
}

// Reset records a successful reset: clears LastApply, updates LastReset.
func (s *Store) Reset(db string, source Source, durationMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.dbs[db]
	cur.Name = db
	cur.LastReset = &Event{At: time.Now(), Source: source, DurationMs: durationMs}
	cur.LastApply = nil
	s.dbs[db] = cur
	return s.commitLocked()
}
