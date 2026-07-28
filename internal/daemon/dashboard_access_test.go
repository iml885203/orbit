package daemon

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDashboardAccessMiddleware_AllowsUnixSocketCLIRequests(t *testing.T) {
	called := false
	handler := dashboardAccessMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodPost, "http://orbit/api/up", nil)
	req = req.WithContext(context.WithValue(
		req.Context(),
		http.LocalAddrContextKey,
		&net.UnixAddr{Name: "/tmp/orbit.sock", Net: "unix"},
	))

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Fatal("unix-socket request was blocked")
	}
}

func TestDashboardAccessMiddleware_AllowsSameOriginMutation(t *testing.T) {
	called := false
	handler := dashboardAccessMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	req := dashboardTCPRequest(http.MethodPost, "localhost:19800")
	req.Header.Set("Origin", "http://localhost:19800")
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called || rr.Code != http.StatusOK {
		t.Fatalf("same-origin mutation called=%v status=%d", called, rr.Code)
	}
}

func TestDashboardAccessMiddleware_BlocksCrossSiteMutation(t *testing.T) {
	called := false
	handler := dashboardAccessMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	req := dashboardTCPRequest(http.MethodPost, "localhost:19800")
	req.Header.Set("Origin", "https://malicious.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called || rr.Code != http.StatusForbidden {
		t.Fatalf("cross-site mutation called=%v status=%d", called, rr.Code)
	}
}

func TestDashboardAccessMiddleware_BlocksDNSRebindingHost(t *testing.T) {
	called := false
	handler := dashboardAccessMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	req := dashboardTCPRequest(http.MethodGet, "malicious.example")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called || rr.Code != http.StatusForbidden {
		t.Fatalf("rebound host called=%v status=%d", called, rr.Code)
	}
}

func TestDashboardAccessMiddleware_AllowsLoopbackToolWithoutBrowserHeaders(t *testing.T) {
	called := false
	handler := dashboardAccessMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	req := dashboardTCPRequest(http.MethodPost, "127.0.0.1:19800")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called || rr.Code != http.StatusOK {
		t.Fatalf("loopback tool called=%v status=%d", called, rr.Code)
	}
}

func dashboardTCPRequest(method, host string) *http.Request {
	req := httptest.NewRequest(method, "http://"+host+"/api/up", nil)
	return req.WithContext(context.WithValue(
		req.Context(),
		http.LocalAddrContextKey,
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19800},
	))
}
