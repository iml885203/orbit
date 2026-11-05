package app

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
)

func TestLogsFollowRequestsTheTailBeforeStreaming(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      []string
		json      bool
		wantQuery string
		want      []string
	}{
		{
			name:      "human default tail",
			args:      []string{"api", "-f"},
			wantQuery: "lines=100",
			want:      []string{"Streaming logs for api (Ctrl+C to stop)...", "backlog line", "live line"},
		},
		{
			name:      "json explicit tail",
			args:      []string{"api", "--follow", "--tail", "37"},
			json:      true,
			wantQuery: "lines=37",
			want:      []string{"backlog line", "live line"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			streamQuery := serveFollowedLogs(t, "backlog line", "live line")
			isolateLogsCommandGlobals(t, tt.json)
			cmd := logsCmd()
			cmd.SetArgs(tt.args)

			output := captureStdout(t, cmd.Execute)

			if got := <-streamQuery; got != tt.wantQuery {
				t.Errorf("stream query = %q, want %q", got, tt.wantQuery)
			}
			var lines []string
			for _, raw := range strings.Split(strings.TrimSpace(output), "\n") {
				if !tt.json {
					lines = append(lines, raw)
					continue
				}
				var event logJSONEvent
				if err := json.Unmarshal([]byte(raw), &event); err != nil {
					t.Fatalf("NDJSON line %q: %v", raw, err)
				}
				if event.Type != "log" || event.Resource != "api" {
					t.Errorf("event = %+v, want a log event for api", event)
				}
				lines = append(lines, event.Line)
			}
			if strings.Join(lines, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("output lines = %q, want %q", lines, tt.want)
			}
		})
	}
}

func TestLogsRejectsNegativeLineCountBeforeContactingTheDaemon(t *testing.T) {
	for _, args := range [][]string{
		{"api", "--lines", "-1"},
		{"api", "--follow", "--tail", "-1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("ORBIT_HOME", t.TempDir())
			isolateLogsCommandGlobals(t, false)
			cmd := logsCmd()
			cmd.SetArgs(args)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true

			if err := cmd.Execute(); !errors.Is(err, cli.ErrInvalidArgument) {
				t.Errorf("err = %v, want an invalid argument error", err)
			}
		})
	}
}

// serveFollowedLogs runs a stub daemon on the Orbit socket whose log stream
// for "api" sends lines and closes, and reports the stream's query string.
func serveFollowedLogs(t *testing.T, lines ...string) <-chan string {
	t.Helper()
	home, err := os.MkdirTemp(shortTestTempRoot(), "o208-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("ORBIT_HOME", home)
	listener, err := net.Listen("unix", daemon.DefaultSocketPath())
	if err != nil {
		t.Fatal(err)
	}
	streamQuery := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(daemon.StatusResponse{Resources: []daemon.ResourceStatus{{Name: "api"}}})
	})
	mux.HandleFunc("/api/logs/api/stream", func(w http.ResponseWriter, r *http.Request) {
		streamQuery <- r.URL.RawQuery
		for _, line := range lines {
			_, _ = w.Write([]byte("data: " + line + "\n\n"))
		}
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return streamQuery
}

func isolateLogsCommandGlobals(t *testing.T, jsonOutput bool) {
	t.Helper()
	previousConfig, previousLines, previousFollow, previousJSON := configFile, logLines, follow, cli.JSONOutput
	t.Cleanup(func() {
		configFile, logLines, follow, cli.JSONOutput = previousConfig, previousLines, previousFollow, previousJSON
	})
	configFile = filepath.Join(t.TempDir(), "missing.yaml")
	cli.JSONOutput = jsonOutput
}
