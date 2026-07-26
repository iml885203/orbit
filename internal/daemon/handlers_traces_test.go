package daemon

import (
	"net"
	"testing"
)

func TestTraceIDLineRegexp(t *testing.T) {
	const id = "0a1b02030405060708090a0b0c0d0e0f"
	re, err := traceIDLineRegexp(id)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	matches := []string{
		`{"Timestamp":"...","TraceId":"` + id + `","Level":"Error"}`,
		`lvl=INFO TraceId=` + id + ` msg=hello`,
		`trace_id=` + id,
		`{"traceId":"` + id + `"}`,
	}
	for _, line := range matches {
		if !re.MatchString(line) {
			t.Errorf("should match: %s", line)
		}
	}

	// Field-anchored: a bare mention of the id (URL, pasted text) is not a
	// join hit, and neither is a different trace's id in the field.
	nonMatches := []string{
		`GET /api/traces/` + id + ` 200`,
		`TraceId=ffff02030405060708090a0b0c0d0e0f`,
		`SpanId=` + id[:16],
	}
	for _, line := range nonMatches {
		if re.MatchString(line) {
			t.Errorf("should NOT match: %s", line)
		}
	}
}

func TestBindOTLPListener_FallsBackPastConflict(t *testing.T) {
	// Occupy the desired port, then let the binder fall back.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy: %v", err)
	}
	defer func() { _ = occupied.Close() }()
	desired := occupied.Addr().(*net.TCPAddr).Port

	ln, port, err := bindOTLPListener(desired, otlpPortFallbackTries)
	if err != nil {
		t.Fatalf("expected fallback to succeed, got %v", err)
	}
	defer func() { _ = ln.Close() }()
	if port == desired {
		t.Errorf("bound the occupied port %d instead of falling back", desired)
	}
	if port <= desired || port >= desired+otlpPortFallbackTries {
		t.Errorf("fallback port %d out of expected range (%d, %d)", port, desired, desired+otlpPortFallbackTries)
	}
}

func TestBindOTLPListener_PinnedPortDoesNotFallBack(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy: %v", err)
	}
	defer func() { _ = occupied.Close() }()
	desired := occupied.Addr().(*net.TCPAddr).Port

	// tries==1 models an explicit (pinned) port: a conflict must surface as an
	// error, not silently move.
	if _, _, err := bindOTLPListener(desired, 1); err == nil {
		t.Error("pinned port conflict should error, not fall back")
	}
}
