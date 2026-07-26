package tunnel

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

// AccessLine is one inbound callback through a tunnel — a one-line summary
// (no body). Emitted by the per-tunnel log-proxy and shown in the dashboard.
type AccessLine struct {
	LocalPort  int       `json:"local_port"`
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	DurationMs int64     `json:"duration_ms"`
}

// AccessLogHub keeps a bounded per-local-port ring of recent AccessLines and
// fans new lines to SSE subscribers. Subscribe replays existing lines (all
// ports) then streams new ones — same contract as history.Recorder.Subscribe.
type AccessLogHub struct {
	mu   sync.Mutex
	cap  int
	ring map[int][]AccessLine // keyed by local port
	subs map[chan AccessLine]struct{}
}

func NewAccessLogHub(ringCap int) *AccessLogHub {
	if ringCap <= 0 {
		ringCap = 200
	}
	return &AccessLogHub{
		cap:  ringCap,
		ring: make(map[int][]AccessLine),
		subs: make(map[chan AccessLine]struct{}),
	}
}

// Record appends a line to the port's ring (trimming to cap) and broadcasts it
// to subscribers (non-blocking; a slow subscriber drops the line).
func (h *AccessLogHub) Record(localPort int, l AccessLine) {
	l.LocalPort = localPort
	h.mu.Lock()
	h.ring[localPort] = append(h.ring[localPort], l)
	if r := h.ring[localPort]; len(r) > h.cap {
		h.ring[localPort] = r[len(r)-h.cap:]
	}
	for ch := range h.subs {
		select {
		case ch <- l:
		default: // slow subscriber; drop (observational stream)
		}
	}
	h.mu.Unlock()
}

// Recent returns a copy of the port's ring (oldest first).
func (h *AccessLogHub) Recent(localPort int) []AccessLine {
	h.mu.Lock()
	defer h.mu.Unlock()
	src := h.ring[localPort]
	out := make([]AccessLine, len(src))
	copy(out, src)
	return out
}

// Subscribe returns a channel that first replays all retained lines, then
// receives new ones, plus an unsubscribe func. Mirrors history.Recorder.
func (h *AccessLogHub) Subscribe() (<-chan AccessLine, func()) {
	h.mu.Lock()
	var replay []AccessLine
	for _, lines := range h.ring {
		replay = append(replay, lines...)
	}
	// Size the buffer to the full cross-port replay (+headroom for live lines
	// arriving during replay). Replay aggregates every port's ring, so a fixed
	// cap-sized buffer would overflow with multiple busy tunnels: the off-lock
	// replay goroutine would block on a full channel and, while blocked, Record's
	// non-blocking sends would drop live lines for the duration of the replay.
	ch := make(chan AccessLine, len(replay)+64)
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	// Replay retained lines outside the lock — with many tunnels the replay can
	// exceed the channel buffer, and blocking the send while holding mu would
	// deadlock concurrent Record calls. Live lines may interleave with replay;
	// acceptable for an observational log (matches history.Recorder semantics).
	// The recover guards against cancel closing ch mid-replay (send on closed
	// channel panics); cancel removes ch from subs under mu, so Record never
	// sends to a closed ch — the replay goroutine is the only off-lock writer.
	go func() {
		defer func() { _ = recover() }()
		for _, l := range replay {
			ch <- l
		}
	}()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// statusRecorder captures the status code written to a ResponseWriter.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// newAccessLogHandler builds the reverse-proxy handler: forward to
// localhost:<localPort>, recording one AccessLine per request into hub.
func newAccessLogHandler(localPort int, hub *AccessLogHub) http.Handler {
	target := &url.URL{Scheme: "http", Host: fmt.Sprintf("localhost:%d", localPort)}
	rp := httputil.NewSingleHostReverseProxy(target)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		method, path := r.Method, r.URL.Path
		sr := &statusRecorder{ResponseWriter: w, status: 200}
		rp.ServeHTTP(sr, r)
		hub.Record(localPort, AccessLine{
			Time:       start,
			Method:     method,
			Path:       path,
			Status:     sr.status,
			DurationMs: time.Since(start).Milliseconds(),
		})
	})
}

// startAccessLogProxy binds a listener on an OS-assigned local port and serves
// the log-proxy in a goroutine. Returns the bound port and a stop func. Fail-
// closed: a bind error is returned so bring-up can fail like any other component.
func startAccessLogProxy(localPort int, hub *AccessLogHub) (boundPort int, stop func(), err error) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, nil, fmt.Errorf("access-log proxy bind: %w", err)
	}
	srv := &http.Server{Handler: newAccessLogHandler(localPort, hub)}
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().(*net.TCPAddr).Port, func() { _ = srv.Close() }, nil
}
