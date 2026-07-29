package health

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestCheck_NilHealthConfig(t *testing.T) {
	checker := NewChecker(nil, nil)
	result := checker.Check(context.Background(), "test-svc", nil)

	if !result.Healthy {
		t.Fatalf("expected healthy, got unhealthy: %s", result.Message)
	}
	if result.Service != "test-svc" {
		t.Fatalf("expected service name %q, got %q", "test-svc", result.Service)
	}
	if result.Message != "no health check configured" {
		t.Fatalf("expected message %q, got %q", "no health check configured", result.Message)
	}
}

func TestCheck_HTTP_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	port := portFromURL(t, srv.URL)

	checker := NewChecker(nil, nil)
	result := checker.Check(context.Background(), "http-ok", &config.HealthCheckConfig{
		Type: "http",
		Port: port,
		Path: "/",
	})

	if !result.Healthy {
		t.Fatalf("expected healthy, got unhealthy: %s", result.Message)
	}
	wantMessage := "HTTP 200 from http://localhost:" + strconv.Itoa(port) + "/"
	if result.Message != wantMessage {
		t.Fatalf("expected message %q, got %q", wantMessage, result.Message)
	}
	if result.Latency <= 0 {
		t.Fatal("expected positive latency")
	}
}

func TestCheck_HTTP_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	port := portFromURL(t, srv.URL)

	checker := NewChecker(nil, nil)
	result := checker.Check(context.Background(), "http-fail", &config.HealthCheckConfig{
		Type: "http",
		Port: port,
		Path: "/",
	})

	if result.Healthy {
		t.Fatal("expected unhealthy, got healthy")
	}
	wantMessage := "HTTP 500 from http://localhost:" + strconv.Itoa(port) + "/"
	if result.Message != wantMessage {
		t.Fatalf("expected message %q, got %q", wantMessage, result.Message)
	}
}

func TestCheck_HTTP_Unreachable(t *testing.T) {
	port := unusedPort(t)

	checker := NewChecker(nil, nil)
	result := checker.Check(context.Background(), "http-down", &config.HealthCheckConfig{
		Type: "http",
		Port: port,
		Path: "/health",
	})

	if result.Healthy {
		t.Fatal("expected unhealthy, got healthy")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestCheck_TCP_Success(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to start TCP listener: %v", err)
	}
	defer func() { _ = ln.Close() }()

	port := ln.Addr().(*net.TCPAddr).Port

	checker := NewChecker(nil, nil)
	result := checker.Check(context.Background(), "tcp-ok", &config.HealthCheckConfig{
		Type: "tcp",
		Port: port,
	})

	if !result.Healthy {
		t.Fatalf("expected healthy, got unhealthy: %s", result.Message)
	}
	if result.Message != "TCP connection OK" {
		t.Fatalf("expected message %q, got %q", "TCP connection OK", result.Message)
	}
}

func TestCheck_TCP_Failure(t *testing.T) {
	port := unusedPort(t)

	checker := NewChecker(nil, nil)
	result := checker.Check(context.Background(), "tcp-fail", &config.HealthCheckConfig{
		Type: "tcp",
		Port: port,
	})

	if result.Healthy {
		t.Fatal("expected unhealthy, got healthy")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestWaitForHealthy_NilConfig(t *testing.T) {
	checker := NewChecker(nil, nil)

	var callbackResult *Result
	err := checker.WaitForHealthy(context.Background(), "nil-cfg", nil, func(r Result) {
		callbackResult = &r
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if callbackResult == nil {
		t.Fatal("expected callback to be invoked")
	}
	if !callbackResult.Healthy {
		t.Fatal("expected healthy result in callback")
	}
}

// portFromURL extracts the port number from an httptest server URL.
func portFromURL(t *testing.T, rawURL string) int {
	t.Helper()
	// URL is like "http://127.0.0.1:PORT"
	_, portStr, err := net.SplitHostPort(rawURL[len("http://"):])
	if err != nil {
		t.Fatalf("failed to parse port from URL %q: %v", rawURL, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("invalid port %q: %v", portStr, err)
	}
	return port
}

// unusedPort finds an ephemeral port that nothing is listening on.
func unusedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to find unused port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}
