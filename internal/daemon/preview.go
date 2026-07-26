package daemon

import (
	"errors"
	"net/http"
)

// ErrPreviewOnly is returned by requireMutable when the active env is
// marked previewOnly: true. The env is inspectable (status, graph, logs)
// but cannot start/stop services, mutate config, or rebuild dev DBs.
var ErrPreviewOnly = errors.New("env is preview-only; only inspection endpoints are allowed")

// requireMutable returns ErrPreviewOnly if the active env's config is
// previewOnly. Call at the top of any handler that starts/stops services,
// mutates config (env toggles, service mode, detached edges, service env),
// or modifies dev databases. Read-only handlers (status, graph, logs,
// settings) must not call this.
func (s *Server) requireMutable() error {
	if s.holder.Load().PreviewOnly {
		return ErrPreviewOnly
	}
	return nil
}

// RejectIfPreview is the exported preview guard for extension-owned
// handlers.
func (s *Server) RejectIfPreview(w http.ResponseWriter) bool {
	return s.rejectIfPreview(w)
}

// rejectIfPreview writes a 409 Conflict and returns true when the active
// env is preview-only. Handlers that would otherwise mutate state call
// this before doing any work.
func (s *Server) rejectIfPreview(w http.ResponseWriter) bool {
	if err := s.requireMutable(); err != nil {
		writeJSON(w, http.StatusConflict, APIResponse{Error: err.Error()})
		return true
	}
	return false
}
