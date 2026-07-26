package daemon

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/cmdmap/gaps"
	"github.com/iml885203/orbit/internal/history"
)

func TestHistoryMiddlewareRecordsUserActionAndGap(t *testing.T) {
	dir := t.TempDir()
	rec, err := history.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rec.Close() }()
	gc := gaps.New(filepath.Join(dir, "gaps.json"))

	mux := http.NewServeMux()
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, APIResponse{OK: true})
	})
	handler := HistoryMiddleware(rec, gc)(mux)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	got := rec.List(history.Filter{})
	if len(got) != 1 {
		t.Fatalf("want latest completed record, got %+v", got)
	}
	if got[0].Status != history.StatusOK {
		t.Fatalf("records newest-first mismatch: %+v", got)
	}
	if gs := gc.List(); len(gs) != 1 || gs[0].PathPattern != "/api/settings" {
		t.Fatalf("gap not tracked: %+v", gs)
	}
}

func TestHistoryMiddlewareDoesNotReadBodyForUnknownRoutes(t *testing.T) {
	dir := t.TempDir()
	rec, err := history.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rec.Close() }()

	body := &countingReadCloser{err: errors.New("body should not be read")}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/unmapped", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, APIResponse{OK: true})
	})
	handler := HistoryMiddleware(rec, nil)(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/unmapped", body)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if body.reads != 0 {
		t.Fatalf("unknown route body was read %d times", body.reads)
	}
	if got := rec.List(history.Filter{}); len(got) != 0 {
		t.Fatalf("unknown route should not be recorded: %+v", got)
	}
}

func TestHistoryMiddlewareRejectsOversizedTrackedBody(t *testing.T) {
	dir := t.TempDir()
	rec, err := history.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rec.Close() }()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, APIResponse{OK: true})
	})
	handler := HistoryMiddleware(rec, nil)(mux)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(strings.Repeat("x", 256*1024+1)))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
}

type countingReadCloser struct {
	reads int
	err   error
}

func (r *countingReadCloser) Read(_ []byte) (int, error) {
	r.reads++
	if r.err != nil {
		return 0, r.err
	}
	return 0, io.EOF
}

func (r *countingReadCloser) Close() error { return nil }

func TestHistoryMiddlewareSkipsCLIOriginRequests(t *testing.T) {
	dir := t.TempDir()
	rec, err := history.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rec.Close() }()
	gc := gaps.New(filepath.Join(dir, "gaps.json"))

	mux := http.NewServeMux()
	mux.HandleFunc("/api/restart/api", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, APIResponse{OK: true})
	})
	handler := HistoryMiddleware(rec, gc)(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/restart/api", nil)
	req.Header.Set(cliOriginHeader, "cli")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rec.List(history.Filter{}); len(got) != 0 {
		t.Fatalf("CLI-origin daemon request should not be recorded as UI action: %+v", got)
	}
	if gs := gc.List(); len(gs) != 0 {
		t.Fatalf("CLI-origin daemon request should not track gaps: %+v", gs)
	}
}
