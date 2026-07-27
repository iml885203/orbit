package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type settingsUpdate struct {
	WorkspaceRoot *string `json:"workspace_root,omitempty"`
	ShowHistory   *bool   `json:"show_history,omitempty"`
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
		record := func(key string, updated *string) {
			if updated == nil {
				return
			}
			changes = append(changes, SettingsChange{Key: key, Old: srv.settings.Get(key), New: *updated})
		}
		record("workspace_root", update.WorkspaceRoot)
		if update.WorkspaceRoot != nil {
			if err := srv.settings.Set("workspace_root", *update.WorkspaceRoot); err != nil {
				slog.Error("persist workspace_root", "component", "settings", "err", err)
				writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
				return
			}
			// Re-export so ${WORKSPACE_ROOT}/${WORKSPACE_ROOT} substitutions pick
			// up the new value on the next config load.
			srv.settings.ApplyToEnv()
		}
		if update.ShowHistory != nil {
			if err := srv.settings.SetShowHistory(update.ShowHistory); err != nil {
				slog.Error("persist show_history", "component", "settings", "err", err)
				writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
				return
			}
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
