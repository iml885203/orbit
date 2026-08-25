package health

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

func TestHTTPProbeDiscoversAndCachesH2C(t *testing.T) {
	var requests atomic.Int32
	port := startH2CServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.ProtoMajor != 2 {
			http.Error(w, "h2c required", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	hc := &config.HealthCheckConfig{Type: "http", Port: port, Path: "/health"}
	session := NewChecker(nil, nil).NewProbeSession(context.Background(), "api", 1, hc)

	for range 2 {
		result := session.Check()
		if !result.Healthy {
			t.Fatalf("h2c probe failed: %s", result.Message)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want one per probe", got)
	}
}

func TestHTTPProbeKeepsHTTP1CompatibilityAndCachesIt(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	hc := &config.HealthCheckConfig{Type: "http", Port: portFromURL(t, srv.URL), Path: "/health"}
	session := NewChecker(nil, nil).NewProbeSession(context.Background(), "api", 1, hc)

	for range 2 {
		result := session.Check()
		if !result.Healthy {
			t.Fatalf("HTTP/1.1 probe failed: %s", result.Message)
		}
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("application requests = %d, want discovery plus one per probe", got)
	}
}

func TestHTTPProbeDoesNotFallbackFromValidH2CFailure(t *testing.T) {
	var http1Requests atomic.Int32
	port := startH2CServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 1 {
			http1Requests.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	hc := &config.HealthCheckConfig{Type: "http", Port: port, Path: "/health"}
	result := NewChecker(nil, nil).NewProbeSession(context.Background(), "api", 1, hc).Check()

	if result.Healthy || !strings.Contains(result.Message, "HTTP 503") {
		t.Fatalf("result = %+v, want authoritative h2c 503", result)
	}
	if got := http1Requests.Load(); got != 0 {
		t.Fatalf("HTTP/1.1 fallback requests = %d, want 0", got)
	}
}

func TestHTTPProbeReportsBothProtocolFailures(t *testing.T) {
	hc := &config.HealthCheckConfig{Type: "http", Port: unusedPort(t), Path: "/health", Timeout: time.Second}
	result := NewChecker(nil, nil).NewProbeSession(context.Background(), "api", 1, hc).Check()

	if result.Healthy || !strings.Contains(result.Message, "h2c:") || !strings.Contains(result.Message, "HTTP/1.1:") {
		t.Fatalf("result = %+v, want both labelled failures", result)
	}
}

func TestHTTPProbeDiscoversEachRedirectOrigin(t *testing.T) {
	targetPort := startH2CServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			http.Error(w, "h2c required", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:"+strconv.Itoa(targetPort)+"/ready", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	hc := &config.HealthCheckConfig{Type: "http", Port: portFromURL(t, source.URL), Path: "/health"}

	result := NewChecker(nil, nil).NewProbeSession(context.Background(), "api", 1, hc).Check()
	if !result.Healthy {
		t.Fatalf("cross-protocol redirect failed: %s", result.Message)
	}
}

func TestHTTPProbeRedirectsFromH2CToHTTP1(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	sourcePort := startH2CServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			http.Error(w, "h2c required", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, target.URL+"/ready", http.StatusTemporaryRedirect)
	}))
	hc := &config.HealthCheckConfig{Type: "http", Port: sourcePort, Path: "/health"}

	result := NewChecker(nil, nil).NewProbeSession(context.Background(), "api", 1, hc).Check()
	if !result.Healthy {
		t.Fatalf("h2c-to-HTTP/1.1 redirect failed: %s", result.Message)
	}
}

func TestHTTPProbeRedirectToHTTPSUsesConfiguredTrust(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/ready", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	checker := NewChecker(nil, nil)
	checker.httpClient = target.Client()
	hc := &config.HealthCheckConfig{Type: "http", Port: portFromURL(t, source.URL), Path: "/health"}

	result := checker.NewProbeSession(context.Background(), "api", 1, hc).Check()
	if !result.Healthy {
		t.Fatalf("trusted HTTPS redirect failed: %s", result.Message)
	}
}

func TestHTTPProbeRejectsRedirectLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	hc := &config.HealthCheckConfig{Type: "http", Port: portFromURL(t, srv.URL), Path: "/health"}
	result := NewChecker(nil, nil).NewProbeSession(context.Background(), "api", 1, hc).Check()
	if result.Healthy || !strings.Contains(result.Message, "stopped after 10 redirects") {
		t.Fatalf("redirect-loop result = %+v", result)
	}
}

func TestHTTPProbeUsesOneTimeoutAcrossRedirectOrigins(t *testing.T) {
	targetStarted := make(chan struct{})
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(targetStarted)
		time.Sleep(60 * time.Millisecond)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer target.Close()
	sourcePort := startH2CServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(30 * time.Millisecond):
			http.Redirect(w, r, target.URL+"/ready", http.StatusTemporaryRedirect)
		case <-r.Context().Done():
		}
	}))
	hc := &config.HealthCheckConfig{Type: "http", Port: sourcePort, Path: "/health", Timeout: 60 * time.Millisecond}
	start := time.Now()
	result := NewChecker(nil, nil).NewProbeSession(context.Background(), "api", 1, hc).Check()
	elapsed := time.Since(start)
	if result.Healthy || !strings.Contains(result.Message, context.DeadlineExceeded.Error()) {
		t.Fatalf("result = %+v", result)
	}
	select {
	case <-targetStarted:
	default:
		t.Fatal("redirect target was not probed")
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("redirect renewed the probe timeout: %s", elapsed)
	}
}

func TestProbeSessionCancellationForcesNextGenerationDiscovery(t *testing.T) {
	var h2cEnabled atomic.Bool
	var h2cRequests atomic.Int32
	var http1Requests atomic.Int32
	port := startH2CServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 {
			h2cRequests.Add(1)
			if !h2cEnabled.Load() {
				panic(http.ErrAbortHandler)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		http1Requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	hc := &config.HealthCheckConfig{Type: "http", Port: port, Path: "/health", Timeout: time.Second}
	checker := NewChecker(nil, nil)
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	first := checker.NewProbeSession(firstCtx, "api", 1, hc)
	if result := first.Check(); !result.Healthy {
		t.Fatalf("first generation failed: %s", result.Message)
	}
	cancelFirst()
	if result := first.Check(); result.Healthy || !strings.Contains(result.Message, context.Canceled.Error()) {
		t.Fatalf("cancelled generation result = %+v", result)
	}

	h2cEnabled.Store(true)
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	second := checker.NewProbeSession(secondCtx, "api", 2, hc)
	if result := second.Check(); !result.Healthy {
		t.Fatalf("second generation failed: %s", result.Message)
	}
	if h2cRequests.Load() < 2 || http1Requests.Load() != 1 {
		t.Fatalf("requests h2c=%d HTTP/1.1=%d, want rediscovery across generations", h2cRequests.Load(), http1Requests.Load())
	}
}

func TestDiscoveryTimeoutDoesNotStartFallback(t *testing.T) {
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	defer cancelLifecycle()
	transport := newProtocolTransport(lifecycleCtx, http.DefaultTransport, 20*time.Millisecond)
	var http1Calls atomic.Int32
	transport.h2c = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, errors.New("h2c stopped")
	})
	transport.http1 = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		http1Calls.Add(1)
		return nil, errors.New("must not run")
	})
	req, _ := http.NewRequest(http.MethodGet, "http://localhost:8080/health", nil)
	_, err := transport.RoundTrip(req)
	if err == nil || !strings.Contains(err.Error(), "h2c stopped") || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("error = %v, want attempt and deadline evidence", err)
	}
	if got := http1Calls.Load(); got != 0 {
		t.Fatalf("HTTP/1.1 fallback calls = %d, want 0", got)
	}
}

func TestLifecycleCancellationStopsLeaderAndWaiters(t *testing.T) {
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	transport := newProtocolTransport(lifecycleCtx, http.DefaultTransport, time.Second)
	started := make(chan struct{})
	var startedOnce sync.Once
	var http1Calls atomic.Int32
	transport.h2c = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		startedOnce.Do(func() { close(started) })
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	transport.http1 = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		http1Calls.Add(1)
		return nil, errors.New("must not run")
	})

	results := make(chan error, 2)
	for range 2 {
		go func() {
			req, _ := http.NewRequest(http.MethodGet, "http://localhost:8080/health", nil)
			_, err := transport.RoundTrip(req)
			results <- err
		}()
	}
	<-started
	cancelLifecycle()
	for range 2 {
		if err := <-results; err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
			t.Fatalf("cancellation error = %v", err)
		}
	}
	if got := http1Calls.Load(); got != 0 {
		t.Fatalf("HTTP/1.1 fallback calls = %d, want 0", got)
	}
}

func TestWaitForHealthyPreservesLastProtocolEvidence(t *testing.T) {
	hc := &config.HealthCheckConfig{
		Type: "http", Port: unusedPort(t), Path: "/health",
		Timeout: 100 * time.Millisecond, Interval: time.Millisecond, Retries: 1,
	}
	checker := NewChecker(nil, nil)
	err := checker.WaitForHealthy(context.Background(), "api", hc, nil)
	if err == nil || !strings.Contains(err.Error(), "h2c:") || !strings.Contains(err.Error(), "HTTP/1.1:") {
		t.Fatalf("wait error = %v, want both protocol failures", err)
	}
	progress := checker.Progress("api")
	if !strings.Contains(progress.LastErr, "h2c:") || !strings.Contains(progress.LastErr, "HTTP/1.1:") {
		t.Fatalf("progress evidence = %q", progress.LastErr)
	}
}

func TestProbeSessionSharesProtocolAcrossStartupAndMonitoring(t *testing.T) {
	var requests atomic.Int32
	reachedMonitor := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 3 {
			close(reachedMonitor)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	hc := &config.HealthCheckConfig{
		Type: "http", Port: portFromURL(t, srv.URL), Path: "/health",
		Timeout: time.Second, Interval: time.Millisecond, Retries: 1,
	}
	ctx, cancel := context.WithCancel(context.Background())
	session := NewChecker(nil, nil).NewProbeSession(ctx, "api", 1, hc)
	if err := session.WaitForHealthy(nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- session.MonitorHealthy(nil) }()
	<-reachedMonitor
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("monitor error = %v", err)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("requests = %d, want one discovery extra then one per phase", got)
	}
}

func TestLifecycleCancellationCannotPublishSuccessfulDiscovery(t *testing.T) {
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	transport := newProtocolTransport(lifecycleCtx, http.DefaultTransport, time.Second)
	started := make(chan struct{})
	release := make(chan struct{})
	transport.h2c = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})
	done := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, "http://localhost:8080/health", nil)
		resp, err := transport.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		done <- err
	}()
	<-started
	cancelLifecycle()
	transport.close()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("discovery error = %v, want lifecycle cancellation", err)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.selected) != 0 {
		t.Fatalf("cancelled lifecycle published protocols: %+v", transport.selected)
	}
}

func TestProtocolDiscoveryIsSingleFlight(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var h2cCalls atomic.Int32
	var http1Calls atomic.Int32
	transport := newProtocolTransport(context.Background(), http.DefaultTransport, time.Second)
	transport.h2c = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		if h2cCalls.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil, errors.New("not h2c")
	})
	transport.http1 = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		http1Calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})

	const callers = 20
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, "http://localhost:8080/health", nil)
			resp, err := transport.RoundTrip(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			errs <- err
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("probe failed: %v", err)
		}
	}
	if got := h2cCalls.Load(); got != 1 {
		t.Fatalf("h2c discovery calls = %d, want 1", got)
	}
	if got := http1Calls.Load(); got != callers {
		t.Fatalf("HTTP/1.1 calls = %d, want %d", got, callers)
	}
}

func TestCancelledDiscoveryWaiterDoesNotCancelLeader(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var h2cCalls atomic.Int32
	transport := newProtocolTransport(context.Background(), http.DefaultTransport, time.Second)
	transport.h2c = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if h2cCalls.Add(1) == 1 {
			close(started)
		}
		<-release
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})

	leaderDone := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, "http://localhost:8080/health", nil)
		resp, err := transport.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		leaderDone <- err
	}()
	<-started

	waiterCtx, cancelWaiter := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(waiterCtx, http.MethodGet, "http://localhost:8080/health", nil)
		resp, err := transport.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		waiterDone <- err
	}()
	cancelWaiter()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter error = %v, want context cancellation", err)
	}
	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader failed: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://localhost:8080/health", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("cached probe failed: %v", err)
	}
	_ = resp.Body.Close()
	if got := h2cCalls.Load(); got != 2 {
		t.Fatalf("h2c calls = %d, want leader discovery plus cached probe", got)
	}
}

func TestCancelledDiscoveryLeaderDoesNotCancelWaiter(t *testing.T) {
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	defer cancelLifecycle()
	release := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int32
	transport := newProtocolTransport(lifecycleCtx, http.DefaultTransport, time.Second)
	transport.h2c = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(leaderCtx, http.MethodGet, "http://localhost:8080/health", nil)
		_, err := transport.RoundTrip(req)
		leaderDone <- err
	}()
	<-started
	waiterDone := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, "http://localhost:8080/health", nil)
		resp, err := transport.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		waiterDone <- err
	}()
	cancelLeader()
	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want cancellation", err)
	}
	close(release)
	if err := <-waiterDone; err != nil {
		t.Fatalf("waiter failed: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("h2c calls = %d, want shared discovery plus waiter request", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func startH2CServer(t *testing.T, handler http.Handler) int {
	t.Helper()
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	protocols := &http.Protocols{}
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{Handler: handler, Protocols: protocols}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	return listener.Addr().(*net.TCPAddr).Port
}
