package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

// TestHandleStop_ReturnsBeforeStopServiceCompletes verifies that the
// stop handler is non-blocking: it acks the request before the actual
// stop work finishes, so the CLI can render progress instead of waiting
// in silence.
func TestHandleStop_ReturnsBeforeStopServiceCompletes(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]*config.Service{
			"svc": {Name: "svc", Type: "node"},
		},
		Containers: map[string]*config.Container{},
	}
	srv := newTestServer(t, cfg)

	// Block StopService inside the orchestrator by making OnStopProcess
	// hang. The handler must still return promptly.
	released := make(chan struct{})
	srv.app.Orchestrator.OnStopProcess = func(name string) error {
		<-released
		return nil
	}

	req := httptest.NewRequest(http.MethodPost, "/api/stop/svc", nil)
	w := httptest.NewRecorder()

	handlerDone := make(chan struct{})
	go func() {
		srv.handleStop(w, req)
		close(handlerDone)
	}()

	select {
	case <-handlerDone:
		// Handler returned. Make sure it was a 200 (the stop is in
		// progress, not failed).
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("handleStop did not return within 500ms; it is still blocking on StopService")
	}

	// Release the inner goroutine and give it time to finish before the
	// test (and t.TempDir cleanup) returns. This avoids a goroutine racing
	// with cleanup under -race.
	close(released)
	// Best-effort wait; if the inner goroutine doesn't finish in 2s
	// something is wrong, but don't fail the test on it — the assertion
	// above is the real signal.
	time.Sleep(200 * time.Millisecond)
}

// TestHandleDown_ReturnsPresentTenseAcknowledgment verifies the down
// handler is non-blocking by checking it returns the present-tense
// "stopping ..." message instead of the past-tense "stopped" wording
// the synchronous implementation used. The message contract is the
// observable signal callers (CLI) depend on.
func TestHandleDown_ReturnsPresentTenseAcknowledgment(t *testing.T) {
	cfg := &config.Config{
		Services:   map[string]*config.Service{"svc": {Name: "svc", Type: "node"}},
		Containers: map[string]*config.Container{},
	}
	srv := newTestServer(t, cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/down", nil)
	w := httptest.NewRecorder()
	srv.handleDown(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "stopping") {
		t.Errorf("expected present-tense 'stopping' in response, got %q", body)
	}
	if strings.Contains(body, `"stopped"`) {
		t.Errorf("response should not say 'stopped' (past tense implies sync completion), got %q", body)
	}

	// Let the inner goroutine finish before t.TempDir cleanup fires.
	time.Sleep(200 * time.Millisecond)
}

// TestHandleRestart_ReturnsPresentTenseAcknowledgment verifies the
// restart handler is non-blocking by checking it returns the present-
// tense "restarting ..." message. The old sync handler returned
// "<name> restarting" — close but inconsistent; we standardise on
// "restarting <name>" matching the stop/down pattern.
func TestHandleRestart_ReturnsPresentTenseAcknowledgment(t *testing.T) {
	cfg := &config.Config{
		Services:   map[string]*config.Service{"svc": {Name: "svc", Type: "node"}},
		Containers: map[string]*config.Container{},
	}
	srv := newTestServer(t, cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/restart/svc", nil)
	w := httptest.NewRecorder()
	srv.handleRestart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "restarting svc") {
		t.Errorf("expected 'restarting svc' in response, got %q", body)
	}

	// Let the inner goroutine finish before t.TempDir cleanup fires.
	time.Sleep(200 * time.Millisecond)
}
