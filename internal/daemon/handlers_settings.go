package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/iml885203/orbit/autoupdate"
)

type settingsUpdate struct {
	ShowHistory      *bool             `json:"show_history,omitempty"`
	UserEnv          map[string]string `json:"user_env,omitempty"`
	AutomaticUpdates *string           `json:"automatic_updates,omitempty"`
}

// handleSettings handles GET (read) and PUT (update) for user settings.
func (srv *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := srv.settings.Snapshot()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
			return
		}
		if launchPath, launchErr := autoupdate.LaunchPath(); launchErr == nil {
			if updateState, stateErr := autoupdate.Load(launchPath); stateErr == nil {
				settings["automatic_updates"] = updateState.Policy
			}
		}
		writeJSON(w, http.StatusOK, settings)

	case http.MethodPut:
		var update settingsUpdate
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
			return
		}
		var changes []SettingsChange
		if update.AutomaticUpdates != nil {
			launchPath, err := autoupdate.LaunchPath()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
				return
			}
			if _, err := autoupdate.SetPolicy(launchPath, *update.AutomaticUpdates); err != nil {
				writeJSON(w, http.StatusBadRequest, APIResponse{Error: err.Error()})
				return
			}
		}
		if update.ShowHistory != nil {
			if err := srv.settings.SetShowHistory(update.ShowHistory); err != nil {
				slog.Error("persist show_history", "component", "settings", "err", err)
				writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
				return
			}
		}
		for key, value := range update.UserEnv {
			changes = append(changes, SettingsChange{Key: "user_env." + key, Old: srv.settings.GetUserEnv(key), New: value})
			if err := srv.settings.SetUserEnv(key, value); err != nil {
				slog.Error("persist user environment variable", "component", "settings", "key", key, "err", err)
				writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
				return
			}
		}
		if len(update.UserEnv) > 0 {
			srv.settings.ApplyToEnv()
		}

		// Feature-owned reactions (the SQL mode switch) run through the
		// registered PUT hooks; a hook that writes the response reports
		// handled. The generic path stays feature-free.
		for _, hook := range srv.settingsPUTHooks {
			if hook(w, changes) {
				return
			}
		}
		writeJSON(w, http.StatusOK, SettingsResponse{OK: true, Message: "settings saved"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Error: "method not allowed"})
	}
}
