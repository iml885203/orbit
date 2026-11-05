package app

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/tracing"
)

const logsTraceID = "0a1b02030405060708090a0b0c0d0e0f"

func fixtureLogPage() tracing.LogPage {
	return tracing.LogPage{
		Logs: []tracing.LogRecord{
			{
				Seq: 5, TimeUnixNano: 2_000_000_000, ObservedTimeUnixNano: 2_000_000_000,
				SeverityNumber: 17, SeverityText: "Error", Body: "payment failed",
				TraceID: logsTraceID, SpanID: "0101010101010101", Service: "api", Scope: "Payments",
				Attributes: map[string]string{
					"exception.type":       "System.InvalidOperationException",
					"exception.message":    "card declined",
					"exception.stacktrace": "System.InvalidOperationException: card declined\n   at Payments.Charge()",
				},
			},
			{Seq: 4, TimeUnixNano: 1_000_000_000, SeverityNumber: 13, Body: "slow query", Service: "api"},
		},
		LatestSeq: 9,
		HasMore:   true,
	}
}

// serveTelemetryDaemon runs a stub daemon on the Orbit socket that answers
// the trace and log-record reads, and reports each log-record query string.
func serveTelemetryDaemon(t *testing.T) <-chan url.Values {
	t.Helper()
	return serveTelemetryDaemonAnswering(t, http.StatusOK, fixtureLogPage())
}

// serveTelemetryDaemonAnswering is serveTelemetryDaemon with the log-record
// response chosen by the test.
func serveTelemetryDaemonAnswering(t *testing.T, logsStatus int, logsBody any) <-chan url.Values {
	t.Helper()
	home, err := os.MkdirTemp(shortTestTempRoot(), "otel-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("ORBIT_HOME", home)
	listener, err := net.Listen("unix", daemon.DefaultSocketPath())
	if err != nil {
		t.Fatal(err)
	}
	queries := make(chan url.Values, 4)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("/api/otel/logs", func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(logsStatus)
		_ = json.NewEncoder(w).Encode(logsBody)
	})
	mux.HandleFunc("/api/traces/"+logsTraceID, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fixtureTrace())
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return queries
}

func isolateTraceCommandGlobals(t *testing.T, jsonOutput bool) {
	t.Helper()
	prevLimit, prevSeverity, prevFilter := logRecordLimit, logRecordSeverity, logRecordFilter
	prevShowLogs, prevJSON := traceShowLogs, cli.JSONOutput
	t.Cleanup(func() {
		logRecordLimit, logRecordSeverity, logRecordFilter = prevLimit, prevSeverity, prevFilter
		traceShowLogs, cli.JSONOutput = prevShowLogs, prevJSON
	})
	cli.JSONOutput = jsonOutput
}

func TestTraceLogsJSONEnvelope(t *testing.T) {
	queries := serveTelemetryDaemon(t)
	isolateTraceCommandGlobals(t, true)
	cmd := traceCmd()
	cmd.SetArgs([]string{"logs", "--service", "api", "--severity", "warning", "--trace", logsTraceID,
		"--search", "Declined", "--since", "3", "--limit", "2"})

	output := captureStdout(t, cmd.Execute)

	q := <-queries
	for key, want := range map[string]string{
		"service": "api", "severity": "13", "trace": logsTraceID, "search": "Declined", "since": "3", "limit": "2",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}

	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		OK            bool   `json:"ok"`
		Data          struct {
			Logs      []map[string]json.RawMessage `json:"logs"`
			LatestSeq int64                        `json:"latestSeq"`
			HasMore   bool                         `json:"hasMore"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("parse envelope: %v\n%s", err, output)
	}
	if envelope.SchemaVersion != cli.SchemaVersion || !envelope.OK ||
		len(envelope.Data.Logs) != 2 || envelope.Data.LatestSeq != 9 || !envelope.Data.HasMore {
		t.Fatalf("envelope = %+v", envelope)
	}
	got := make(map[string]bool)
	for k := range envelope.Data.Logs[0] {
		got[k] = true
	}
	assertExactKeys(t, got, []string{
		"seq", "timeUnixNano", "observedTimeUnixNano", "severityNumber", "severityText",
		"body", "traceId", "spanId", "service", "scope", "attributes",
	})
}

func TestTraceLogsSendsSinceOnlyWhenGiven(t *testing.T) {
	for _, tc := range []struct {
		args      []string
		wantSince string
		wantHas   bool
	}{
		{[]string{"logs", "--since", "0"}, "0", true},
		{[]string{"logs"}, "", false},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			queries := serveTelemetryDaemon(t)
			isolateTraceCommandGlobals(t, true)
			cmd := traceCmd()
			cmd.SetArgs(tc.args)

			captureStdout(t, cmd.Execute)

			if q := <-queries; q.Has("since") != tc.wantHas || q.Get("since") != tc.wantSince {
				t.Errorf("query = %v, want since present=%v value %q", q, tc.wantHas, tc.wantSince)
			}
		})
	}
}

func TestTraceLogsFooterFlagsACursorTheDaemonDidNotIssue(t *testing.T) {
	page := fixtureLogPage()
	page.CursorReset = true
	serveTelemetryDaemonAnswering(t, http.StatusOK, page)
	isolateTraceCommandGlobals(t, false)
	color.NoColor = true
	cmd := traceCmd()
	cmd.SetArgs([]string{"logs", "--since", "99999"})

	output := captureStdout(t, cmd.Execute)

	if !strings.Contains(output, "--since 99999 was not issued by this daemon") ||
		!strings.Contains(output, "More follow: --since 5.") {
		t.Errorf("footer should flag the reset cursor and keep paging:\n%s", output)
	}
}

func TestTraceLogsRejectsBadFlagsBeforeContactingTheDaemon(t *testing.T) {
	for _, args := range [][]string{
		{"logs", "--severity", "loud"},
		{"logs", "--since", "-1"},
		{"logs", "--limit", "-1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("ORBIT_HOME", t.TempDir())
			isolateTraceCommandGlobals(t, false)
			cmd := traceCmd()
			cmd.SetArgs(args)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true

			if err := cmd.Execute(); !errors.Is(err, cli.ErrInvalidArgument) {
				t.Errorf("err = %v, want an invalid argument error", err)
			}
		})
	}
}

func TestTraceLogsHumanOutputReadsLikeATail(t *testing.T) {
	serveTelemetryDaemon(t)
	isolateTraceCommandGlobals(t, false)
	color.NoColor = true
	cmd := traceCmd()
	cmd.SetArgs([]string{"logs"})

	lines := strings.Split(strings.TrimSpace(captureStdout(t, cmd.Execute)), "\n")

	want := []string{"WARN  api slow query", "ERROR api payment failed trace=" + logsTraceID,
		"    System.InvalidOperationException: card declined", "       at Payments.Charge()", "",
		"2 record(s). Older matches withheld by --limit. Newer records later: --since 9."}
	if len(lines) != len(want) {
		t.Fatalf("output:\n%s", strings.Join(lines, "\n"))
	}
	for i, w := range want {
		if !strings.HasSuffix(lines[i], w) {
			t.Errorf("line %d = %q, want suffix %q", i, lines[i], w)
		}
	}
}

func TestTraceShowWithLogsJoinsLogRecords(t *testing.T) {
	queries := serveTelemetryDaemon(t)
	isolateTraceCommandGlobals(t, true)
	cmd := traceCmd()
	cmd.SetArgs([]string{logsTraceID, "--logs"})

	output := captureStdout(t, cmd.Execute)

	if q := <-queries; q.Get("trace") != logsTraceID || q.Get("limit") != "0" {
		t.Errorf("log-record query = %v, want every record of the trace", q)
	}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("parse envelope: %v\n%s", err, output)
	}
	var logs []tracing.LogRecord
	_ = json.Unmarshal(envelope.Data["logs"], &logs)
	if envelope.Data["traceId"] == nil || envelope.Data["spans"] == nil || len(logs) != 2 {
		t.Errorf("data should be the trace's fields plus its log records: %s", output)
	}
}

func TestTraceShowWithLogsJSONKeepsTheTraceWhenLogRecordsFail(t *testing.T) {
	serveTelemetryDaemonAnswering(t, http.StatusInternalServerError, daemon.APIResponse{Error: "log store exploded"})
	isolateTraceCommandGlobals(t, true)
	cmd := traceCmd()
	cmd.SetArgs([]string{logsTraceID, "--logs"})

	output := captureStdout(t, cmd.Execute)

	var envelope struct {
		OK      bool                       `json:"ok"`
		Data    map[string]json.RawMessage `json:"data"`
		Notices []cli.JSONNotice           `json:"notices"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("parse envelope: %v\n%s", err, output)
	}
	if !envelope.OK || envelope.Data["traceId"] == nil || string(envelope.Data["logs"]) != "[]" {
		t.Errorf("want ok with the trace and logs: [], got %s", output)
	}
	if len(envelope.Notices) != 1 || envelope.Notices[0].Code != "log_records_unavailable" ||
		!strings.Contains(envelope.Notices[0].Message, "log store exploded") {
		t.Errorf("notices = %+v, want one log_records_unavailable carrying the daemon error", envelope.Notices)
	}
}
