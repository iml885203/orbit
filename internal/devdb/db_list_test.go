package devdb

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
)

// serveDBListDaemon runs a fake daemon on the default socket answering the
// requests `sqlserver list` makes, with the raw camelCase db-state body the
// real daemon sends.
func serveDBListDaemon(t *testing.T, dbStateStatus int, dbStateBody string) {
	t.Helper()
	home, err := os.MkdirTemp(shortSocketTempRoot(), "o225-")
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
	mux.HandleFunc("/api/health", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("/api/devdb/meta", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"db_configured":true}`))
	})
	mux.HandleFunc("/api/devdb/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"name":"Orders","path":"db/Orders/Orders.sqlproj","databases":["OrdersDB","OrdersArchiveDB"]},{"name":"Billing","path":"db/Billing/Billing.sqlproj","databases":["BillingDB","OrdersDB"]}]}`))
	})
	mux.HandleFunc("/api/db-state", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(dbStateStatus)
		_, _ = w.Write([]byte(dbStateBody))
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
}

func shortSocketTempRoot() string {
	if runtime.GOOS == "darwin" {
		return "/tmp"
	}
	return os.TempDir()
}

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func() error) ([]byte, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	runErr := fn()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return out, runErr
}

func TestDBListCommandJSONReportsDaemonPublishRecords(t *testing.T) {
	origJSON := cli.JSONOutput
	t.Cleanup(func() { cli.JSONOutput = origJSON })
	cli.JSONOutput = true
	serveDBListDaemon(t, http.StatusOK, `{"version":1,"updatedAt":"2026-11-04T10:00:00Z","dbs":{
		"OrdersDB":{"name":"OrdersDB","lastPublish":{"at":"2026-11-04T09:12:03Z","source":"cli","durationMs":8412},"baselineAt":{"at":"2026-11-04T09:12:05Z","source":"cli"}},
		"BillingDB":{"name":"BillingDB","lastReset":{"at":"2026-11-04T10:00:00Z","source":"ui","durationMs":900},"lastPublish":{"at":"2026-11-04T10:00:00Z","source":"ui","durationMs":900},"baselineAt":{"at":"2026-11-04T10:00:00Z","source":"ui"}},
		"RetiredDB":{"name":"RetiredDB","lastPublish":{"at":"2026-11-01T09:00:00Z","source":"ui"}}}}`)

	out, err := captureStdout(t, func() error { return runDBList(nil, nil) })
	if err != nil {
		t.Fatalf("runDBList: %v", err)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Projects       []map[string]any             `json:"projects"`
			PublishRecords []map[string]json.RawMessage `json:"publish_records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if !env.OK || len(env.Data.Projects) != 2 || env.Data.Projects[0]["name"] != "Orders" {
		t.Fatalf("envelope = %s", out)
	}
	if len(env.Data.PublishRecords) != 3 {
		t.Fatalf("want one record per configured database, got %s", out)
	}
	for _, rec := range env.Data.PublishRecords {
		for _, key := range []string{"database", "project", "last_publish", "last_reset", "baseline_at"} {
			if _, ok := rec[key]; !ok {
				t.Fatalf("record missing key %q: %s", key, out)
			}
		}
	}
	names := make([]string, 0, len(env.Data.PublishRecords))
	for _, rec := range env.Data.PublishRecords {
		var database, project string
		if err := json.Unmarshal(rec["database"], &database); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(rec["project"], &project); err != nil {
			t.Fatal(err)
		}
		names = append(names, database+"@"+project)
	}
	if strings.Join(names, ",") != "OrdersDB@Orders,OrdersArchiveDB@Orders,BillingDB@Billing" {
		t.Fatalf("configured database ownership/deduplication = %v", names)
	}
	compact := func(raw json.RawMessage) string {
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw); err != nil {
			t.Fatalf("compact: %v", err)
		}
		return buf.String()
	}
	if got := compact(env.Data.PublishRecords[0]["last_publish"]); got != `{"at":"2026-11-04T09:12:03Z","source":"cli","duration_ms":8412}` {
		t.Fatalf("OrdersDB last_publish = %s", got)
	}
	if got := compact(env.Data.PublishRecords[0]["baseline_at"]); got != `{"at":"2026-11-04T09:12:05Z","source":"cli"}` {
		t.Fatalf("OrdersDB baseline_at = %s", got)
	}
	if got := compact(env.Data.PublishRecords[1]["last_publish"]); got != "null" {
		t.Fatalf("unrecorded last_publish = %s, want explicit null", got)
	}
	for _, field := range []string{"last_publish", "last_reset"} {
		if got := compact(env.Data.PublishRecords[2][field]); got != `{"at":"2026-11-04T10:00:00Z","source":"ui","duration_ms":900}` {
			t.Fatalf("reset must retain paired publish: %s = %s", field, got)
		}
	}
	if got := compact(env.Data.PublishRecords[2]["baseline_at"]); got != `{"at":"2026-11-04T10:00:00Z","source":"ui"}` {
		t.Fatalf("reset baseline = %s", got)
	}

}

func TestDBListCommandJSONFailsWhenRecordUnavailable(t *testing.T) {
	origJSON := cli.JSONOutput
	t.Cleanup(func() { cli.JSONOutput = origJSON })
	cli.JSONOutput = true
	serveDBListDaemon(t, http.StatusInternalServerError, `{"error":"boom"}`)

	out, err := captureStdout(t, func() error { return runDBList(nil, nil) })
	if err == nil {
		t.Fatalf("runDBList succeeded without a publish record: %s", out)
	}
	if len(out) != 0 {
		t.Fatalf("a failed list must not write a success envelope: %s", out)
	}
}
