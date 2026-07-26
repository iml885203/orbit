package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// stubDaemon spins up an httptest.Server backed by handler and returns a
// *Client whose transport dials that server regardless of hostname. This
// allows tests to exercise client methods end-to-end without a real unix
// socket or daemon process.
//
// The server is closed automatically via t.Cleanup.
func stubDaemon(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	return &Client{
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "tcp", base.Host)
				},
			},
		},
	}
}

func TestStubDaemon(t *testing.T) {
	c := stubDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	if err := c.Health(); err != nil {
		t.Fatalf("stubDaemon Health() = %v; want nil", err)
	}
}

func TestSetEdgeDetached(t *testing.T) {
	type capture struct {
		method string
		path   string
		body   EdgeDetachRequest
	}

	var got []capture

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
		}
		var req EdgeDetachRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("unmarshal body: %v", err)
		}
		got = append(got, capture{method: r.Method, path: r.URL.Path, body: req})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":null}`))
	})

	c := stubDaemon(t, handler)

	if err := c.SetEdgeDetached("payments", "worker", true); err != nil {
		t.Fatalf("SetEdgeDetached(detached=true) = %v; want nil", err)
	}
	if err := c.SetEdgeDetached("payments", "worker", false); err != nil {
		t.Fatalf("SetEdgeDetached(detached=false) = %v; want nil", err)
	}

	if len(got) != 2 {
		t.Fatalf("want 2 requests captured, got %d", len(got))
	}

	for i, tc := range []struct {
		wantDetached bool
	}{
		{wantDetached: true},
		{wantDetached: false},
	} {
		cap := got[i]
		if cap.method != http.MethodPut {
			t.Errorf("[%d] method = %q; want PUT", i, cap.method)
		}
		if cap.path != "/api/edges/payments/worker" {
			t.Errorf("[%d] path = %q; want /api/edges/payments/worker", i, cap.path)
		}
		if cap.body.Detached != tc.wantDetached {
			t.Errorf("[%d] body.Detached = %v; want %v", i, cap.body.Detached, tc.wantDetached)
		}
	}
}

func TestReadError_DecodeFailure(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader("not json at all")),
	}
	err := readError(resp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 500") {
		t.Errorf("error %q missing status code", msg)
	}
	if !strings.Contains(msg, "decode") {
		t.Errorf("error %q missing decode context", msg)
	}
}

func TestReadError_APIErrorField(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":"bad input"}`)),
	}
	err := readError(resp)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "bad input" {
		t.Errorf("got %q, want %q", err.Error(), "bad input")
	}
}

func TestReadError_EmptyObject_FallsBackToStatus(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}
	err := readError(resp)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "HTTP 404" {
		t.Errorf("got %q, want %q", err.Error(), "HTTP 404")
	}
}

func TestSetServiceMode(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody ServiceModeRequest
	c := stubDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	if err := c.SetServiceMode("worker", "container"); err != nil {
		t.Fatalf("SetServiceMode: %v", err)
	}
	if gotMethod != "PUT" || gotPath != "/api/service-mode/worker" {
		t.Errorf("method=%q path=%q", gotMethod, gotPath)
	}
	if gotBody.Mode != "container" {
		t.Errorf("mode = %q", gotBody.Mode)
	}
}

func TestSetEnvToggle(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody EnvToggleUpdateRequest
	c := stubDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	if err := c.SetEnvToggle("api", "FEATURE_X", true); err != nil {
		t.Fatalf("SetEnvToggle: %v", err)
	}
	if gotMethod != "PUT" || gotPath != "/api/env-toggles" {
		t.Errorf("method=%q path=%q", gotMethod, gotPath)
	}
	if gotBody.Service != "api" || gotBody.Var != "FEATURE_X" || !gotBody.Enabled {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestGetSettings(t *testing.T) {
	c := stubDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/settings" {
			t.Errorf("method=%q path=%q", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sql_server_image":"example.db:latest","show_history":true}`))
	}))

	got, err := c.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got["sql_server_image"] != "example.db:latest" {
		t.Errorf("sql_server_image = %v", got["sql_server_image"])
	}
	if got["show_history"] != true {
		t.Errorf("show_history = %v", got["show_history"])
	}
}

func TestUpdateSettings(t *testing.T) {
	var gotBody map[string]any
	c := stubDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("method = %q", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	if err := c.UpdateSettings(map[string]any{"show_history": false}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if gotBody["show_history"] != false {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestDialReturnsErrDaemonUnreachableWhenSocketMissing(t *testing.T) {
	_, err := Dial("/tmp/orbit-test-socket-does-not-exist.sock")
	if err == nil {
		t.Fatal("expected error when socket does not exist, got nil")
	}
	if !errors.Is(err, ErrDaemonUnreachable) {
		t.Fatalf("expected errors.Is(err, ErrDaemonUnreachable) = true, got false; err = %v", err)
	}
}

func TestDialErrorMessageContainsHint(t *testing.T) {
	_, err := Dial("/tmp/orbit-test-socket-does-not-exist.sock")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "orbit up") {
		t.Errorf("expected hint mentioning 'orbit up' in error, got: %s", err.Error())
	}
}
