package devdb

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/dbstate"
)

func TestDBListJSONCarriesPublishRecordPerConfiguredDatabase(t *testing.T) {
	published := time.Date(2026, 11, 4, 9, 12, 3, 0, time.UTC)
	reset := published.Add(time.Hour)
	projects := []DevDBProject{
		{Name: "Orders", Path: "db/Orders/Orders.sqlproj", Databases: []string{"OrdersDB", "OrdersArchiveDB"}},
		{Name: "Billing", Path: "db/Billing/Billing.sqlproj", Databases: []string{"BillingDB", "OrdersDB"}},
	}
	snap := dbstate.Snapshot{DBs: map[string]dbstate.DBState{
		"OrdersDB": {
			Name:        "OrdersDB",
			LastPublish: &dbstate.Event{At: published, Source: dbstate.SourceCLI, DurationMs: 8412},
			BaselineAt:  &dbstate.Event{At: published, Source: dbstate.SourceCLI},
		},
		// A reset records the clean publish it performs alongside the reset.
		"BillingDB": {
			Name:        "BillingDB",
			LastReset:   &dbstate.Event{At: reset, Source: dbstate.SourceUI, DurationMs: 900},
			LastPublish: &dbstate.Event{At: reset, Source: dbstate.SourceUI, DurationMs: 900},
			BaselineAt:  &dbstate.Event{At: reset, Source: dbstate.SourceUI},
		},
		"RetiredDB": {Name: "RetiredDB", LastPublish: &dbstate.Event{At: published, Source: dbstate.SourceCLI}},
	}}

	got := dbListJSON(projects, snap)

	if len(got.Projects) != 2 {
		t.Fatalf("projects changed: %+v", got.Projects)
	}
	names := make([]string, 0, len(got.PublishRecords))
	for _, r := range got.PublishRecords {
		names = append(names, r.Database+"@"+r.Project)
	}
	want := []string{"OrdersDB@Orders", "OrdersArchiveDB@Orders", "BillingDB@Billing"}
	if len(names) != len(want) {
		t.Fatalf("records = %v, want %v (configured only, duplicate under first project)", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("records = %v, want %v", names, want)
		}
	}

	orders := got.PublishRecords[0]
	if orders.LastPublish == nil || !orders.LastPublish.At.Equal(published) || orders.LastPublish.DurationMs != 8412 || orders.BaselineAt == nil || orders.LastReset != nil {
		t.Fatalf("OrdersDB record = %+v", orders)
	}
	archive := got.PublishRecords[1]
	if archive.LastPublish != nil || archive.LastReset != nil || archive.BaselineAt != nil {
		t.Fatalf("unrecorded database must carry no events: %+v", archive)
	}
	billing := got.PublishRecords[2]
	if billing.LastReset == nil || billing.LastPublish == nil || billing.LastPublish.Source != dbstate.SourceUI {
		t.Fatalf("reset record must carry its paired publish: %+v", billing)
	}
}

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
		_, _ = w.Write([]byte(`{"projects":[{"name":"Orders","path":"db/Orders/Orders.sqlproj","databases":["OrdersDB","OrdersArchiveDB"]}]}`))
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
	if !env.OK || len(env.Data.Projects) != 1 || env.Data.Projects[0]["name"] != "Orders" {
		t.Fatalf("envelope = %s", out)
	}
	if len(env.Data.PublishRecords) != 2 {
		t.Fatalf("want one record per configured database, got %s", out)
	}
	for _, rec := range env.Data.PublishRecords {
		for _, key := range []string{"database", "project", "last_publish", "last_reset", "baseline_at"} {
			if _, ok := rec[key]; !ok {
				t.Fatalf("record missing key %q: %s", key, out)
			}
		}
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
