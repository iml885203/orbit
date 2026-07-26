package tunnel

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAccessLogHub_RecordAndRecent(t *testing.T) {
	h := NewAccessLogHub(3) // ring cap 3 per port
	for i := 0; i < 5; i++ {
		h.Record(8080, AccessLine{Method: "POST", Path: "/callbacks/x", Status: 200, DurationMs: int64(i)})
	}
	got := h.Recent(8080)
	if len(got) != 3 {
		t.Fatalf("want 3 (ring cap), got %d", len(got))
	}
	if got[0].DurationMs != 2 || got[2].DurationMs != 4 {
		t.Errorf("ring should keep newest; got durations %d..%d", got[0].DurationMs, got[2].DurationMs)
	}
}

func TestAccessLogHub_RecentEmptyForUnknownPort(t *testing.T) {
	h := NewAccessLogHub(10)
	if got := h.Recent(9999); len(got) != 0 {
		t.Errorf("want empty for unknown port, got %d", len(got))
	}
}

func TestAccessLogHub_SubscribeReplaysThenStreams(t *testing.T) {
	h := NewAccessLogHub(10)
	h.Record(8080, AccessLine{Method: "GET", Path: "/a", Status: 200, LocalPort: 8080})

	ch, cancel := h.Subscribe()
	defer cancel()

	select {
	case l := <-ch:
		if l.Path != "/a" {
			t.Errorf("replay path = %q, want /a", l.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("expected replayed line")
	}

	h.Record(8080, AccessLine{Method: "POST", Path: "/b", Status: 201, LocalPort: 8080})
	select {
	case l := <-ch:
		if l.Path != "/b" {
			t.Errorf("live path = %q, want /b", l.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("expected live line")
	}
}

func TestAccessLogHub_SubscribeBufferHoldsMultiPortReplay(t *testing.T) {
	// Replay aggregates every port's ring. With several busy tunnels the total
	// exceeds the old fixed cap+64 buffer; the buffer must size to the actual
	// replay so the off-lock replay goroutine never blocks and Record never drops
	// the live line that follows.
	h := NewAccessLogHub(200)
	const ports, perPort = 4, 200
	for p := 0; p < ports; p++ {
		for i := 0; i < perPort; i++ {
			h.Record(9000+p, AccessLine{Method: "POST", Path: "/x", Status: 200, DurationMs: int64(i)})
		}
	}

	ch, cancel := h.Subscribe()
	defer cancel()

	// A live line recorded right after subscribe must not be dropped even though
	// the replay (ports*perPort = 800 lines) is still draining.
	h.Record(9000, AccessLine{Method: "GET", Path: "/live", Status: 201, LocalPort: 9000})

	deadline := time.After(2 * time.Second)
	var replayed, sawLive int
	for replayed+sawLive < ports*perPort+1 {
		select {
		case l := <-ch:
			if l.Path == "/live" {
				sawLive++
			} else {
				replayed++
			}
		case <-deadline:
			t.Fatalf("timed out: replayed=%d (want %d), sawLive=%d", replayed, ports*perPort, sawLive)
		}
	}
	if replayed != ports*perPort {
		t.Errorf("replayed %d lines, want %d", replayed, ports*perPort)
	}
	if sawLive != 1 {
		t.Errorf("live line dropped during replay: sawLive=%d", sawLive)
	}
}

func TestAccessLogHub_CancelUnsubscribes(t *testing.T) {
	h := NewAccessLogHub(10)
	ch, cancel := h.Subscribe()
	cancel()
	// after cancel, Record must not panic on a closed channel
	h.Record(8080, AccessLine{Method: "GET", Path: "/a", Status: 200})
	_ = ch
}

func TestAccessLogProxy_ForwardsAndRecords(t *testing.T) {
	// upstream "local service"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = io.WriteString(w, "nope")
	}))
	defer upstream.Close()
	upstreamPort := portOfURL(t, upstream.URL)

	hub := NewAccessLogHub(10)
	handler := newAccessLogHandler(upstreamPort, hub) // proxies to localhost:<upstreamPort>

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/callbacks/provider-a/getbalance", strings.NewReader("x"))
	handler.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("proxy should pass through upstream status, got %d", rec.Code)
	}
	lines := hub.Recent(upstreamPort)
	if len(lines) != 1 {
		t.Fatalf("want 1 recorded line, got %d", len(lines))
	}
	if lines[0].Method != "POST" || lines[0].Path != "/callbacks/provider-a/getbalance" || lines[0].Status != 404 {
		t.Errorf("bad recorded line: %+v", lines[0])
	}
	if lines[0].Time.IsZero() {
		t.Errorf("line should be timestamped")
	}
}

func portOfURL(t *testing.T, u string) int {
	t.Helper()
	i := strings.LastIndex(u, ":")
	if i < 0 {
		t.Fatalf("no port in %q", u)
	}
	var p int
	if _, err := fmt.Sscanf(u[i+1:], "%d", &p); err != nil {
		t.Fatalf("parse port from %q: %v", u, err)
	}
	return p
}
