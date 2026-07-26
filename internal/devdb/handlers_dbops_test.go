package devdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
)

// Each test uses a minimal dbFeature with just enough wiring to drive
// the handlers without actually spawning docker / dotnet — the handlers
// short-circuit on missing prereqs before any subprocess runs.

func newDBOpsTestServer(t *testing.T) (*dbFeature, *http.ServeMux) {
	srv := newTestDBFeature(t, &config.Config{
		Containers: map[string]*config.Container{
			"sql-server": {Name: "sql-server", Image: "irrelevant"},
		},
	})
	srv.dbOps = newDBOpsManager()
	mux := http.NewServeMux()
	registerDBPublishHandler(mux, srv)
	return srv, mux
}

func TestHandleDBOpPublish_InvalidJSON_400(t *testing.T) {
	_, mux := newDBOpsTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/db/publish", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestHandleDBOpPublish_InvalidName_400(t *testing.T) {
	_, mux := newDBOpsTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/db/publish",
		strings.NewReader(`{"db":"bad-name!"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestSafeDBName(t *testing.T) {
	cases := map[string]bool{
		"AppDB":       true,
		"_underscore": true,
		"a1b2":        true,
		"":            false,
		"1leading":    false,
		"has-dash":    false,
		"has space":   false,
		"semi;colon":  false,
	}
	for name, want := range cases {
		if got := safeDBName.MatchString(name); got != want {
			t.Errorf("safeDBName.MatchString(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestDBOpFrame_JSONShape_OmitsZeroOptionalsButNotOK(t *testing.T) {
	f := DBOpFrame{Kind: "output", Line: "hello"}
	b, _ := json.Marshal(f)
	s := string(b)
	if !strings.Contains(s, `"kind":"output"`) || !strings.Contains(s, `"line":"hello"`) {
		t.Errorf("unexpected JSON: %s", s)
	}
	// Empty optional fields should be omitted.
	if strings.Contains(s, `"db":""`) {
		t.Errorf("zero-value db not omitted: %s", s)
	}
	// OK must always be emitted (not omitempty) so consumers see explicit success/failure on done frames.
	doneFrame := DBOpFrame{Kind: "done", OK: false, DurationMs: 100, Err: "boom"}
	d, _ := json.Marshal(doneFrame)
	if !strings.Contains(string(d), `"ok":false`) {
		t.Errorf("done frame must emit ok:false explicitly, got: %s", d)
	}
}
