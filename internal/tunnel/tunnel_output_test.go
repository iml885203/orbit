package tunnel

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestTunnelOutputConnectedMatchesTulJSONContract(t *testing.T) {
	var buffer bytes.Buffer
	newTunnelOutput(&buffer, "json").connected([]string{"/foo"}, 8080, true)

	var event map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["schema_version"] != float64(1) || event["type"] != "connected" ||
		event["target"] != "localhost:8080" || event["background"] != true {
		t.Fatalf("event = %#v", event)
	}
	if event["release_command"] != "orbit tunnel release --to 8080" {
		t.Fatalf("release command = %#v", event["release_command"])
	}
}

func TestTunnelOutputStreamsTulStyleActivity(t *testing.T) {
	var buffer bytes.Buffer
	out := newTunnelOutput(&buffer, "text")
	out.request(AccessLine{Method: "POST", Path: "/foo", Status: 204, DurationMs: 42})
	if got := buffer.String(); got != "→ POST /foo  204  42ms\n" {
		t.Fatalf("activity = %q", got)
	}
}

func TestTunnelOutputEmptyListMatchesTulGuidance(t *testing.T) {
	var buffer bytes.Buffer
	newTunnelOutput(&buffer, "text").tunnels(TunnelListResponse{})
	if !strings.Contains(buffer.String(), "Use --all to include others") {
		t.Fatalf("output = %q", buffer.String())
	}
}

func TestTunnelOutputReleaseSummaryUsesActualCount(t *testing.T) {
	var buffer bytes.Buffer
	newTunnelOutput(&buffer, "json").releaseSummary(8080, 3, "https://gateway.example/_tunlease")
	var event map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["released"] != float64(3) || event["failed"] != float64(0) ||
		event["gateway"] != "https://gateway.example/_tunlease" {
		t.Fatalf("event = %#v", event)
	}
}

func TestTunnelOutputListMatchesTulJSONContract(t *testing.T) {
	var buffer bytes.Buffer
	newTunnelOutput(&buffer, "json").tunnels(TunnelListResponse{Claims: []LocalClaimView{{
		Paths: []string{"/foo"}, Owner: "orbit", StartedAt: "2026-07-26T12:00:00Z",
		LocalPort: 8080, Status: "healthy",
	}}})
	var event struct {
		Claims []map[string]any `json:"claims"`
	}
	if err := json.Unmarshal(buffer.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	claim := event.Claims[0]
	for _, field := range []string{"paths", "owner", "started_at", "mine", "status", "target", "local_port"} {
		if _, ok := claim[field]; !ok {
			t.Fatalf("missing %s in %#v", field, claim)
		}
	}
}

func TestContainsEvery(t *testing.T) {
	if !containsEvery([]string{"/a", "/b"}, []string{"/b"}) {
		t.Fatal("expected subset match")
	}
	if containsEvery([]string{"/a"}, []string{"/b"}) {
		t.Fatal("unexpected subset match")
	}
}
