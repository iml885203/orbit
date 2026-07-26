package devdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/dbstate"
)

// newDBStateTestMux wires a fresh store into a gated (sql-server-configured)
// test server and returns the mux plus the store for direct assertions.
func newDBStateTestMux(t *testing.T) (*http.ServeMux, *dbstate.Store) {
	t.Helper()
	mux := http.NewServeMux()
	store, err := dbstate.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := newTestDBFeature(t, sqlServerConfig())
	srv.dbState = store
	registerDBStateHandlers(mux, srv)
	return mux, store
}

func TestHandleDBState_EventApply_UpdatesSnapshot(t *testing.T) {
	mux, _ := newDBStateTestMux(t)

	body := strings.NewReader(`{"kind":"apply","db":"AppDB","source":"cli","status":"ok","durationMs":1234}`)
	req := httptest.NewRequest(http.MethodPost, "/api/db-state/event", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("event status = %d, body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/db-state", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var snap dbstate.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if snap.DBs["AppDB"].LastApply == nil {
		t.Error("LastApply should be set after event")
	}
}

func TestHandleDBState_EventErrorStatus_DoesNotMutate(t *testing.T) {
	mux, store := newDBStateTestMux(t)

	body := strings.NewReader(`{"kind":"apply","db":"AppDB","source":"cli","status":"error","errorMsg":"boom"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/db-state/event", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if len(store.Snapshot().DBs) != 0 {
		t.Error("error event must not create state")
	}
}

func TestHandleDBState_EventBadJSON_400(t *testing.T) {
	mux, _ := newDBStateTestMux(t)

	req := httptest.NewRequest(http.MethodPost, "/api/db-state/event", strings.NewReader(`not-json`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestHandleDBState_EventMissingFields_400(t *testing.T) {
	mux, _ := newDBStateTestMux(t)

	req := httptest.NewRequest(http.MethodPost, "/api/db-state/event", strings.NewReader(`{"kind":"apply"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}
