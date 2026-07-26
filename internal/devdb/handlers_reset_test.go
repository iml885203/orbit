package devdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iml885203/orbit/daemon"
)

// The reset endpoint's request-shape validation before any SQL Server probe.
func TestHandleDBOpReset_RequestValidation(t *testing.T) {
	s := newTestDBFeature(t, sqlServerConfig())

	cases := []struct {
		name string
		body string
		want string
	}{
		{"invalid json", `not json`, "invalid json"},
		{"removed acknowledgement", `{"db":"SomeDB","acknowledgeRecreate":true}`, "invalid json"},
		{"bad db name", `{"db":"x; DROP"}`, "invalid db name"},
		{"missing db", `{}`, "invalid db name"},
	}
	for _, tc := range cases {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/db/reset", strings.NewReader(tc.body))
		s.handleDBOpReset(rr, req)
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
