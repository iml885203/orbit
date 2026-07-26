package devdb

import (
	"bytes"
	"strings"
	"sync"
	"time"
)

// dbOpKind names the DB operation in the wire shape — the SSE start
// frame carries it as Op so the dashboard can tell the two verbs apart.
type dbOpKind string

const (
	dbOpPublish dbOpKind = "publish"
	dbOpReset   dbOpKind = "reset"
)

// DBOpFrame is one `dbop` SSE event on the multiplexed /api/events
// stream. Exactly one
// field group is populated per Kind.
type DBOpFrame struct {
	Kind       string    `json:"kind"`                 // "idle" | "start" | "output" | "done"
	Op         string    `json:"op,omitempty"`         // start: "publish"
	DB         string    `json:"db,omitempty"`         // start
	All        bool      `json:"all,omitempty"`        // start, publish only: publishing every database
	StartedAt  time.Time `json:"startedAt,omitempty"`  // start
	Line       string    `json:"line,omitempty"`       // output
	OK         bool      `json:"ok"`                   // done (always emitted so false is explicit)
	DurationMs int64     `json:"durationMs,omitempty"` // done
	Err        string    `json:"err,omitempty"`        // done (only when !OK)
	ErrorCode  string    `json:"errorCode,omitempty"`  // done (only when !OK): stable hint key
}

// dbOp is the active operation snapshot — kept so late subscribers
// can catch up on accumulated output.
type dbOp struct {
	Kind      dbOpKind
	DB        string
	All       bool // publish only: publishing every database
	StartedAt time.Time
	Output    []string // accumulated lines
}

// dbOpsManager serialises daemon-initiated publish operations
// (UI button clicks + CLI publish). One op runs at a time across
// the whole daemon; concurrent attempts get LockOrReject = false.
//
// Subscribe uses a 512-frame buffer and drops only when an extremely stalled
// subscriber overflows it (frames are incremental output lines, not snapshots
// — coalescing would lose data). See Subscribe for the rationale.
type dbOpsManager struct {
	mu   sync.Mutex // guards op, subs, lineBuf
	op   *dbOp      // non-nil iff op in flight
	subs map[chan DBOpFrame]struct{}

	// lineBuf accumulates partial bytes between Write calls so a
	// dotnet-build chunk that splits a line mid-buffer still emits
	// one frame per line.
	lineBuf bytes.Buffer
}

func newDBOpsManager() *dbOpsManager {
	return &dbOpsManager{subs: map[chan DBOpFrame]struct{}{}}
}

// LockOrReject claims the op slot. Returns false if another op is in
// flight. Caller MUST call Finish (success or failure) to release.
// all marks a publish that spans every database (db is then empty).
func (m *dbOpsManager) LockOrReject(kind dbOpKind, db string, all bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.op != nil {
		return false
	}
	m.op = &dbOp{Kind: kind, DB: db, StartedAt: time.Now(), All: all}
	m.lineBuf.Reset()
	frame := DBOpFrame{
		Kind:      "start",
		Op:        string(kind),
		DB:        db,
		All:       m.op.All,
		StartedAt: m.op.StartedAt,
	}
	m.broadcastLocked(frame)
	return true
}

// Write satisfies io.Writer. Splits on '\n' and broadcasts one frame
// per complete line. Trailing partial line is buffered until the next
// Write or Finish.
func (m *dbOpsManager) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.op == nil {
		// Defensive: shouldn't happen — caller wouldn't write to a
		// detached manager. Drop the bytes rather than panic.
		return len(p), nil
	}
	m.lineBuf.Write(p)
	for {
		raw := m.lineBuf.Bytes()
		i := bytes.IndexByte(raw, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(raw[:i]), "\r")
		m.lineBuf.Next(i + 1)
		m.op.Output = append(m.op.Output, line)
		m.broadcastLocked(DBOpFrame{Kind: "output", Line: line})
	}
	return len(p), nil
}

// Finish marks the op done with the given outcome and releases the
// slot. Safe to call even if Write left a partial line — that
// partial line is flushed first. errorCode is the stable code the
// dashboard maps to an actionable hint (publish path; empty for
// legacy ops).
func (m *dbOpsManager) Finish(ok bool, durationMs int64, err, errorCode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.op == nil {
		return
	}
	// Flush trailing partial line.
	if rest := strings.TrimRight(m.lineBuf.String(), "\r\n"); rest != "" {
		m.op.Output = append(m.op.Output, rest)
		m.broadcastLocked(DBOpFrame{Kind: "output", Line: rest})
	}
	m.lineBuf.Reset()
	m.broadcastLocked(DBOpFrame{
		Kind:       "done",
		OK:         ok,
		DurationMs: durationMs,
		Err:        err,
		ErrorCode:  errorCode,
	})
	m.op = nil
}

// InFlight returns the active op summary (Kind, DB) or empty Kind if idle.
func (m *dbOpsManager) InFlight() (kind dbOpKind, db string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.op == nil {
		return "", ""
	}
	return m.op.Kind, m.op.DB
}

// Subscribe returns a channel that receives frames. On subscribe:
//   - idle: sends one {Kind:"idle"} frame
//   - in-flight: sends {Kind:"start", ...} + replay of all
//     accumulated output frames
//
// Caller MUST invoke cancel when done.
//
// Drop policy: frames are line-incremental output, not snapshots, so coalescing
// (the dbstate pattern) would lose data. Instead we use a generous
// buffer (512) sized for a full dotnet build's output and accept that an
// extremely stalled subscriber gets dropped — keeps the daemon healthy if a
// client wedges entirely. Realistic SSE pipes drain well within 512 frames.
func (m *dbOpsManager) Subscribe() (<-chan DBOpFrame, func()) {
	ch := make(chan DBOpFrame, 512)
	m.mu.Lock()
	if m.op == nil {
		ch <- DBOpFrame{Kind: "idle"}
	} else {
		ch <- DBOpFrame{
			Kind:      "start",
			Op:        string(m.op.Kind),
			DB:        m.op.DB,
			All:       m.op.All,
			StartedAt: m.op.StartedAt,
		}
		for _, line := range m.op.Output {
			ch <- DBOpFrame{Kind: "output", Line: line}
		}
	}
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		if _, ok := m.subs[ch]; ok {
			delete(m.subs, ch)
			close(ch)
		}
		m.mu.Unlock()
	}
}

func (m *dbOpsManager) broadcastLocked(frame DBOpFrame) {
	for ch := range m.subs {
		select {
		case ch <- frame:
		default:
			close(ch)
			delete(m.subs, ch)
		}
	}
}
