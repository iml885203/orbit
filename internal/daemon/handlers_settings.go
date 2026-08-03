package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type settingsUpdate struct {
	ShowHistory *bool             `json:"show_history,omitempty"`
	UserEnv     map[string]string `json:"user_env,omitempty"`
}

// handleSettings handles GET (read) and PUT (update) for user settings.
func (srv *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, srv.settings)

	case http.MethodPut:
		var update settingsUpdate
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
			return
		}
		var changes []SettingsChange
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
