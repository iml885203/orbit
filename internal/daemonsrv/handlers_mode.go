package daemonsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
)

func (s *Server) handleServiceMode(w http.ResponseWriter, r *http.Request) {
	if !s.beginLifecycleRequest(w, r) {
		return
	}
	defer s.environmentTransitionMu.Unlock()
	if requireMethod(w, r, http.MethodPut) {
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

	kind := "service"
	if req.Mode == "container" {
		kind = "container"
	}
	s.snapshotMu.RLock()
	operation := daemon.LifecycleOperation{Kind: "mode", TargetMode: req.Mode, Epoch: s.epoch(), Generation: s.environmentGeneration.Load(), ConfigPath: s.ConfigPath(), Instance: s.instanceName, Resources: []string{name}}
	s.snapshotMu.RUnlock()
	operation, err := s.lifecycle.accept(operation)
	if err != nil {
		lifecycleAcceptanceError(w, err)
		return
	}
	if err := s.settings.SetServiceMode(name, req.Mode); err != nil {
		s.lifecycle.discardAdmission(operation.ID)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}
	s.dispatchLifecycle(operation, func(id string) error {
		// Group grace and the subsequent join/drain are separate waits. Keep the
		// previous transition allowance beyond both, without duration overflow.
		grace := s.holder.Load().Settings.ShutdownTimeout
		budget := time.Duration(1<<63 - 1)
		if grace <= (budget-2*time.Minute)/2 {
			budget = 2*grace + 2*time.Minute
		}
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		defer cancel()
		err := s.app.Orchestrator.ChangeServiceMode(ctx, name, kind)
		if err == nil {
			s.lifecycle.phase(id, "starting")
		}
		s.PersistState()
		return err
	})
	writeJSON(w, http.StatusOK, APIResponse{OK: true, OperationID: operation.ID, AffectedResources: []string{name}, Message: fmt.Sprintf("%s switching to %s mode", name, req.Mode)})
}

func runtimeServiceMode(cfg *config.Config, name, kind string) string {
	if !isDualDefined(cfg, name) {
		return ""
	}
	if kind == "container" {
		return "container"
	}
	return "dev"
}
