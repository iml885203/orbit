package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

// An HTTP check honours its configured timeout. `tcp` always did; `http` was
// capped by a fixed client deadline, so a documented `timeout:` longer than
// that silently did nothing.
func TestCheck_HTTPHonorsConfiguredTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(release)

	port := portFromURL(t, srv.URL)
	checker := NewChecker(nil, nil)

	// A timeout well under the old fixed 5s must be what actually fires.
	start := time.Now()
	result := checker.Check(context.Background(), "slow", &config.HealthCheckConfig{
		Type:    "http",
		Port:    port,
		Path:    "/",
		Timeout: 300 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if result.Healthy {
		t.Fatal("a probe that never gets a response must be unhealthy")
	}
	if elapsed > 2*time.Second {
		t.Errorf("configured 300ms timeout did not fire; gave up after %s", elapsed)
	}
}
