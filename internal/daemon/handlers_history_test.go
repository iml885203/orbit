package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/cmdmap/gaps"
	"github.com/iml885203/orbit/internal/history"
)

func TestHistoryListAndGapsHandlers(t *testing.T) {
	dir := t.TempDir()
	rec, err := history.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rec.Close() }()
	gc := gaps.New(filepath.Join(dir, "gaps.json"))
	rec.Record(history.Record{ID: "1", Timestamp: time.Now(), Source: history.SourceUI, Path: "/api/up", Summary: "up", HasCLI: true, Status: history.StatusOK})
	rec.Record(history.Record{ID: "2", Timestamp: time.Now(), Source: history.SourceUI, Path: "/api/settings", Summary: "settings", HasCLI: false, Status: history.StatusOK})
	gc.Track("PUT", "/api/settings", "settings")

	mux := http.NewServeMux()
	registerHistoryHandlers(mux, rec, gc)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/history/list?onlyNoCli=true")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var records []history.Record
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ID != "2" {
		t.Fatalf("records: %+v", records)
	}
}
