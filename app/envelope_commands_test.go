package app

import (
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/iml885203/orbit/daemon"
)

func TestDaemonStatusJSONReportsAStoppedDaemonInTheEnvelope(t *testing.T) {
	useJSONOutput(t)
	t.Setenv("ORBIT_HOME", t.TempDir())
	finishCapture := captureLifecycleProcessStreams(t)

	if err := runDaemonStatus(nil, nil); err != nil {
		t.Fatal(err)
	}
	output, _ := finishCapture()

	data := envelopeData(t, output)
	if data["running"] != false {
		t.Fatalf("data = %v, want running false", data)
	}
}

// serveHistoryDaemon answers the two history reads the way a daemon with no
// recorded gaps does: one record, and a null gap list.
func serveHistoryDaemon(t *testing.T) {
	t.Helper()
	home, err := os.MkdirTemp(shortTestTempRoot(), "history-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("ORBIT_HOME", home)
	listener, err := net.Listen("unix", daemon.DefaultSocketPath())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/history/list", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"h1","source":"cli","command":"orbit up","hasCLI":true,"status":"ok"}]`))
	})
	mux.HandleFunc("/api/history/gaps", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`null`))
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
}

func TestHistoryJSONWrapsRecordsAndGapsInTheEnvelope(t *testing.T) {
	useJSONOutput(t)
	serveHistoryDaemon(t)

	t.Run("records", func(t *testing.T) {
		finishCapture := captureLifecycleProcessStreams(t)
		cmd := historyCmd()
		cmd.SetArgs(nil)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		output, _ := finishCapture()

		data := envelopeData(t, output)
		records, ok := data["records"].([]any)
		if !ok || len(records) != 1 {
			t.Fatalf("data = %v, want one record under records", data)
		}
		if record := records[0].(map[string]any); record["command"] != "orbit up" {
			t.Fatalf("record = %v, want the daemon's record unchanged", record)
		}
	})

	t.Run("gaps", func(t *testing.T) {
		finishCapture := captureLifecycleProcessStreams(t)
		cmd := historyCmd()
		cmd.SetArgs([]string{"gaps"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		output, _ := finishCapture()

		data := envelopeData(t, output)
		gaps, ok := data["gaps"].([]any)
		if !ok || len(gaps) != 0 {
			t.Fatalf("data = %v, want an empty gaps array rather than null", data)
		}
	})
}
