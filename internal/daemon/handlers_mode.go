package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleServiceMode(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodPut) {
		return
	}
	if s.rejectIfPreview(w) {
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/service-mode/")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "service name required"})
		return
	}

	if !isDualDefined(s.holder.Load(), name) {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: fmt.Sprintf("%s is not available in both dev and container mode", name)})
		return
	}

	var req ServiceModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid request body"})
		return
	}
	if req.Mode != "dev" && req.Mode != "container" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "mode must be 'dev' or 'container'"})
		return
	}

	if err := s.settings.SetServiceMode(name, req.Mode); err != nil {
		slog.Error("persist service mode", "component", "settings", "err", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	kind := "service"
	if req.Mode == "container" {
		kind = "container"
	}
	s.app.Orchestrator.SetServiceKind(name, kind)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := s.app.Orchestrator.RestartService(ctx, name); err != nil {
			slog.Error("restart failed", "component", "service-mode", "name", name, "err", err)
		}
	}()

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: fmt.Sprintf("%s switching to %s mode", name, req.Mode)})
}
