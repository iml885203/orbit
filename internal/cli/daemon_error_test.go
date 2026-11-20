package cli

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/internal/daemon"
)

func TestDaemonCodeJSON(t *testing.T) {
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "s"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/service-mode/missing" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"unknown resource: missing","code":"unknown_resource"}`))
	}))
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	err = daemon.NewClient(listener.Addr().String()).SetServiceMode("missing", "container")
	if err == nil {
		t.Fatal("daemon failure returned success")
	}
	var out bytes.Buffer
	if err := WriteJSONError(&out, "orbit service mode missing container --json", fmt.Errorf("mode change: %w", err)); err != nil {
		t.Fatal(err)
	}
	got := decodeEnvelope(t, out.Bytes())
	if got.OK || got.SchemaVersion != "orbit.cli.v1" || got.Error == nil || got.Error.Code != "unknown_resource" || got.Error.Retryable {
		t.Fatalf("envelope = %+v", got)
	}
	if len(got.RecommendedActions) != 1 || got.RecommendedActions[0].Command != "orbit status --json" {
		t.Fatalf("recommended actions = %+v", got.RecommendedActions)
	}
}
