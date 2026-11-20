package daemon

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type clientResponseCall struct {
	name, method, path string
	invoke             func(*Client) error
}

func clientResponseCalls() []clientResponseCall {
	return []clientResponseCall{
		{"Doctor", http.MethodGet, "/api/doctor", func(c *Client) error { _, err := c.Doctor(); return err }},
		{"Status", http.MethodGet, "/api/status", func(c *Client) error { _, err := c.Status(); return err }},
		{"Envs", http.MethodGet, "/api/envs", func(c *Client) error { _, err := c.Envs(); return err }},
		{"Version", http.MethodGet, "/api/version", func(c *Client) error { _, err := c.Version(); return err }},
		{"SetEdgeDetached", http.MethodPut, "/api/edges/api/worker", func(c *Client) error { return c.SetEdgeDetached("api", "worker", true) }},
		{"SetServiceMode", http.MethodPut, "/api/service-mode/api", func(c *Client) error { return c.SetServiceMode("api", "container") }},
		{"SetEnvToggle", http.MethodPut, "/api/env-toggles", func(c *Client) error { return c.SetEnvToggle("api", "FEATURE_X", true) }},
		{"UpdateSettings", http.MethodPut, "/api/settings", func(c *Client) error { return c.UpdateSettings(map[string]any{"show_history": false}) }},
		{"GetDecode", http.MethodGet, "/api/env/preview", func(c *Client) error { var out EnvironmentApplyPreview; return c.GetDecode("/api/env/preview", &out) }},
		{"PostDecode", http.MethodPost, "/api/env/reconcile", func(c *Client) error {
			var out EnvironmentReconcileResponse
			return c.PostDecode("/api/env/reconcile", struct{}{}, &out)
		}},
		{"Up", http.MethodPost, "/api/up", func(c *Client) error { _, err := c.Up(UpRequest{Resources: []string{"api"}}); return err }},
	}
}

type clientResponseTransport struct {
	base   http.RoundTripper
	closed *atomic.Bool
}

func (p clientResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := p.base.RoundTrip(req)
	if err == nil {
		resp.Body = clientResponseBody{ReadCloser: resp.Body, closed: p.closed}
	}
	return resp, err
}

type clientResponseBody struct {
	io.ReadCloser
	closed *atomic.Bool
}

func (p clientResponseBody) Close() error {
	p.closed.Store(true)
	return p.ReadCloser.Close()
}

func clientResponseFixture(t *testing.T, call clientResponseCall, status int, body string) *Client {
	t.Helper()
	c := stubDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != call.method || r.URL.Path != call.path {
			t.Errorf("request = %s %s, want %s %s", r.Method, r.URL.Path, call.method, call.path)
		}
		if r.Header.Get(UpdateTransactionHeader) != "probe-update" {
			t.Errorf("update transaction header = %q", r.Header.Get(UpdateTransactionHeader))
		}
		if r.Method != http.MethodGet {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("content type = %q", r.Header.Get("Content-Type"))
			}
			if r.Header.Get(CLIOriginHeader) != "cli" {
				t.Errorf("CLI origin header = %q", r.Header.Get(CLIOriginHeader))
			}
			if r.Header.Get(ExpectedEpochHeader) != "123" || r.Header.Get(ExpectedGenerationHeader) != "7" {
				t.Errorf("environment precondition headers = %q/%q", r.Header.Get(ExpectedEpochHeader), r.Header.Get(ExpectedGenerationHeader))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	closed := &atomic.Bool{}
	c.http.Transport = clientResponseTransport{base: c.http.Transport, closed: closed}
	c.transactionID = "probe-update"
	t.Cleanup(func() {
		if !closed.Load() {
			t.Error("response body was not closed")
		}
	})
	return c.WithEnvironment(123, 7)
}

func TestClientPublicMethodsPreserveDaemonErrorCode(t *testing.T) {
	for _, call := range clientResponseCalls() {
		t.Run(call.name, func(t *testing.T) {
			c := clientResponseFixture(t, call, http.StatusNotFound, `{"error":"unknown resource: missing","code":"unknown_resource"}`)
			err := call.invoke(c)
			if err == nil {
				t.Fatal("request returned success for daemon HTTP 404 error")
			}
			var coded interface{ ErrorCode() string }
			if !errors.As(err, &coded) {
				t.Fatalf("error = %v (%T), want stable daemon error code", err, err)
			}
			if coded.ErrorCode() != "unknown_resource" {
				t.Errorf("error code = %q, want unknown_resource", coded.ErrorCode())
			}
			if errors.Is(err, ErrDaemonUnreachable) || errors.Is(err, ErrDaemonTimeout) {
				t.Errorf("HTTP response error was misclassified as transport failure: %v", err)
			}
		})
	}
}

func TestClientRejectsHTTPFailureWithoutErrorField(t *testing.T) {
	for _, call := range clientResponseCalls() {
		t.Run(call.name, func(t *testing.T) {
			c := clientResponseFixture(t, call, http.StatusServiceUnavailable, `{}`)
			if err := call.invoke(c); err == nil {
				t.Fatal("request returned success for HTTP 503 with an empty JSON object")
			}
		})
	}
}

func TestClientPublicMethodsKeepSuccessAndMalformedResponseHandling(t *testing.T) {
	for _, call := range clientResponseCalls() {
		t.Run(call.name, func(t *testing.T) {
			t.Run("success", func(t *testing.T) {
				c := clientResponseFixture(t, call, http.StatusOK, `{"ok":true}`)
				if err := call.invoke(c); err != nil {
					t.Fatalf("success response: %v", err)
				}
			})
			t.Run("uncoded failure", func(t *testing.T) {
				c := clientResponseFixture(t, call, http.StatusBadRequest, `{"error":"invalid request"}`)
				err := call.invoke(c)
				if err == nil || !strings.Contains(err.Error(), "invalid request") {
					t.Fatalf("error = %v, want daemon message", err)
				}
				var coded interface{ ErrorCode() string }
				if errors.As(err, &coded) {
					t.Fatalf("uncoded response acquired code %q", coded.ErrorCode())
				}
			})
			t.Run("malformed failure", func(t *testing.T) {
				c := clientResponseFixture(t, call, http.StatusBadGateway, `not JSON`)
				if err := call.invoke(c); err == nil {
					t.Fatal("malformed HTTP failure returned success")
				}
			})
			t.Run("malformed", func(t *testing.T) {
				c := clientResponseFixture(t, call, http.StatusOK, `not JSON`)
				if err := call.invoke(c); err == nil {
					t.Fatal("malformed response returned success")
				}
			})
		})
	}
}

func TestClientAPIResponsePreservesAcceptedAndFailurePayloads(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusAccepted} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			call := clientResponseCall{name: "PostJSON", method: http.MethodPost, path: "/api/db/publish"}
			c := clientResponseFixture(t, call, status, `{"ok":true,"message":"scheduled","operation_id":"op-1","affected_resources":["api"]}`)
			result, err := c.PostJSON(call.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.OK || result.Message != "scheduled" || result.OperationID != "op-1" || len(result.AffectedResources) != 1 || result.AffectedResources[0] != "api" {
				t.Fatalf("result = %+v", result)
			}
		})
	}
	t.Run("failure payload", func(t *testing.T) {
		call := clientResponseCall{name: "Up", method: http.MethodPost, path: "/api/up"}
		c := clientResponseFixture(t, call, http.StatusConflict, `{"error":"unknown resource: missing","code":"unknown_resource","affected_resources":["api"]}`)
		result, err := c.Up(UpRequest{})
		if err == nil || result == nil || result.Code != "unknown_resource" || len(result.AffectedResources) != 1 {
			t.Fatalf("result = %+v, error = %v", result, err)
		}
	})
}

func TestClientPublicMethodsKeepCancellationCause(t *testing.T) {
	for _, call := range clientResponseCalls() {
		t.Run(call.name, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			c := stubDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			t.Cleanup(func() { close(release) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- call.invoke(c.WithContext(ctx)) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("request did not reach server")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v, want canceled cause", err)
				}
				if errors.Is(err, ErrDaemonTimeout) {
					t.Fatalf("cancellation classified as timeout: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("request ignored context cancellation")
			}
		})
	}
}

func TestClientTypedGETPreservesPayload(t *testing.T) {
	t.Run("Doctor", func(t *testing.T) {
		c := clientResponseFixture(t, clientResponseCall{method: http.MethodGet, path: "/api/doctor"}, http.StatusOK, `{"ran_at":"now","checks":[{"name":"Docker"}]}`)
		result, err := c.Doctor()
		if err != nil || result == nil || result.RanAt != "now" || len(result.Checks) != 1 || result.Checks[0].Name != "Docker" {
			t.Fatalf("result = %+v, error = %v", result, err)
		}
	})
	t.Run("Status", func(t *testing.T) {
		c := clientResponseFixture(t, clientResponseCall{method: http.MethodGet, path: "/api/status"}, http.StatusOK, `{"epoch":123,"generation":7,"config_path":"project.yaml","resources":[{"name":"api"}]}`)
		result, err := c.Status()
		if err != nil || result == nil || result.Epoch != 123 || result.Generation != 7 || result.ConfigPath != "project.yaml" || len(result.Resources) != 1 || result.Resources[0].Name != "api" {
			t.Fatalf("result = %+v, error = %v", result, err)
		}
	})
	t.Run("Envs", func(t *testing.T) {
		c := clientResponseFixture(t, clientResponseCall{method: http.MethodGet, path: "/api/envs"}, http.StatusOK, `{"current":"project.yaml","running":2}`)
		result, err := c.Envs()
		if err != nil || result == nil || result.Current != "project.yaml" || result.Running != 2 {
			t.Fatalf("result = %+v, error = %v", result, err)
		}
	})
	t.Run("Version", func(t *testing.T) {
		c := clientResponseFixture(t, clientResponseCall{method: http.MethodGet, path: "/api/version"}, http.StatusOK, `{"running":"v1.0.0","on_disk":"v1.1.0","update_available":true}`)
		result, err := c.Version()
		if err != nil || result == nil || result.Running != "v1.0.0" || result.OnDisk != "v1.1.0" || !result.UpdateAvailable {
			t.Fatalf("result = %+v, error = %v", result, err)
		}
	})
}
