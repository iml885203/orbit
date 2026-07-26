package devdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iml885203/orbit/daemon"
)

// The publish endpoint's request-shape validation: all and db are
// mutually exclusive ways to scope the run, and a single-db request
// still goes through the name gate.
func TestHandleDBOpPublish_RequestValidation(t *testing.T) {
	s := newTestDBFeature(t, sqlServerConfig())

	cases := []struct {
		name string
		body string
		want string
	}{
		{"all and db together", `{"all":true,"db":"SomeDB"}`, "mutually exclusive"},
		{"bad db name", `{"db":"x; DROP"}`, "invalid db name"},
		{"missing db without all", `{}`, "invalid db name"},
		{"removed mode", `{"db":"SomeDB","mode":"clean"}`, "invalid json"},
	}
	for _, tc := range cases {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/db/publish", strings.NewReader(tc.body))
		s.handleDBOpPublish(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", tc.name, rr.Code)
			continue
		}
		var resp daemon.APIResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: decode: %v", tc.name, err)
		}
		if !strings.Contains(resp.Error, tc.want) {
			t.Errorf("%s: error = %q, want it to mention %q", tc.name, resp.Error, tc.want)
		}
	}
}
