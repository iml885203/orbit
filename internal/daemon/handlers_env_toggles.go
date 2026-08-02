package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// EnvToggleInfo is the API response type for a single toggle.
type EnvToggleInfo struct {
	Service     string `json:"service"`
	Var         string `json:"var"`
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Default     bool   `json:"default"`
}

func (srv *Server) handleEnvToggles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		srv.environmentTransitionMu.RLock()
		defer srv.environmentTransitionMu.RUnlock()
		toggles := []EnvToggleInfo{}
		cfg := srv.holder.Load()
		for svcName, svc := range cfg.Services {
			for varName, toggle := range svc.EnvToggles {
				key := svcName + "/" + varName
				enabled := srv.settings.IsEnvToggleOn(key, toggle.Default)
				toggles = append(toggles, EnvToggleInfo{
					Service:     svcName,
					Var:         varName,
					Value:       svc.Env[varName],
					Label:       toggle.Label,
					Description: toggle.Description,
					Enabled:     enabled,
					Default:     toggle.Default,
				})
			}
		}
		writeJSON(w, http.StatusOK, toggles)

	case http.MethodPut:
		srv.environmentTransitionMu.Lock()
		defer srv.environmentTransitionMu.Unlock()
		identity := srv.environmentContext().Identity
		var req EnvToggleUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
			return
		}
		key := req.Service + "/" + req.Var
		if err := srv.settings.SetEnvToggle(key, req.Enabled); err != nil {
			slog.Error("persist env toggle", "component", "settings", "err", err)
			writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
			return
		}
		// Only restart if the service is currently running. Toggling env
		// on a stopped service should just persist the new value; starting
		// it implicitly would surprise the user (they didn't ask to bring
		// it up). Next `orbit up` picks up the new toggle naturally.
		isRunning := false
		if info, ok := srv.app.Orchestrator.GetServiceInfo(req.Service); ok {
			st := info.State.String()
			isRunning = st != "stopped" && st != "pending"
		}
		if isRunning {
			srv.startEnvironmentBackground(identity, func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				if err := srv.app.RestartService(ctx, req.Service); err != nil {
					slog.Error("restart failed", "component", "env-toggles", "service", req.Service, "err", err)
				}
			})
		}
		state := "ON"
		if !req.Enabled {
			state = "OFF"
		}
		msg := fmt.Sprintf("%s → %s", req.Var, state)
		if isRunning {
			msg += fmt.Sprintf(" — restarting %s", req.Service)
		} else {
			msg += " — saved (will apply on next start)"
		}
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: msg})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Error: "method not allowed"})
	}
}
