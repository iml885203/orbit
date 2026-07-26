package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
)

// previewServer returns a test server whose active env is preview-only.
func previewServer(t *testing.T) *Server {
	t.Helper()
	cfg := testConfig()
	cfg.PreviewOnly = true
	return newTestServer(t, cfg)
}

// mutationHandlers lists each (handler, http method, path) we expect to
// reject when the active env is preview-only. Keep in sync with
// preview.go: every handler that calls rejectIfPreview must appear here.
type mutationHandler struct {
	name    string
	method  string
	path    string
	body    string
	handler func(s *Server) http.HandlerFunc
}

func mutationHandlers() []mutationHandler {
	return []mutationHandler{
		{
			name: "up", method: http.MethodPost, path: "/api/up", body: `{}`,
			handler: func(s *Server) http.HandlerFunc { return s.handleUp },
		},
		{
			name: "down", method: http.MethodPost, path: "/api/down", body: `{}`,
			handler: func(s *Server) http.HandlerFunc { return s.handleDown },
		},
		{
			name: "stop", method: http.MethodPost, path: "/api/stop/api", body: ``,
			handler: func(s *Server) http.HandlerFunc { return s.handleStop },
		},
		{
			name: "restart", method: http.MethodPost, path: "/api/restart/api", body: ``,
			handler: func(s *Server) http.HandlerFunc { return s.handleRestart },
		},
		{
			name: "env-toggles", method: http.MethodPut, path: "/api/env-toggles",
			body:    `{"service":"api","var":"X","enabled":true}`,
			handler: func(s *Server) http.HandlerFunc { return s.handleEnvToggles },
		},
		{
			name: "service-mode", method: http.MethodPut, path: "/api/service-mode/api",
			body:    `{"mode":"dev"}`,
			handler: func(s *Server) http.HandlerFunc { return s.handleServiceMode },
		},
		{
			name: "edge-detach", method: http.MethodPut, path: "/api/edges/api/db",
			body:    `{"detached":true}`,
			handler: func(s *Server) http.HandlerFunc { return s.handleEdgeDetach },
		},
	}
}

func TestPreviewOnly_RejectsMutationHandlers(t *testing.T) {
	for _, h := range mutationHandlers() {
		t.Run(h.name, func(t *testing.T) {
			s := previewServer(t)
			req := httptest.NewRequest(h.method, h.path, strings.NewReader(h.body))
			rr := httptest.NewRecorder()
			h.handler(s)(rr, req)
			if rr.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409; body=%s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "preview-only") {
				t.Errorf("body missing 'preview-only': %s", rr.Body.String())
			}
		})
	}
}

func TestPreviewOnly_AllowsInspectionHandlers(t *testing.T) {
	// Sanity check: read endpoints still work on a preview-only env.
	// We exercise the ones that don't need a running orchestrator.
	s := previewServer(t)

	t.Run("status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		rr := httptest.NewRecorder()
		s.handleStatus(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("env-toggles-get", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/env-toggles", nil)
		rr := httptest.NewRecorder()
		s.handleEnvToggles(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestRequireMutable_EmptyConfig(t *testing.T) {
	// The holder invariant replaces the old nil-cfg defensiveness: NewServer
	// always receives a NewHolder(non-nil cfg), so Load never returns nil.
	s := &Server{holder: config.NewHolder(&config.Config{})}
	if err := s.requireMutable(); err != nil {
		t.Errorf("non-preview config should be mutable: %v", err)
	}
}
