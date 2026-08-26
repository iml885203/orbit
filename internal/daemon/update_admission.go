package daemon

import (
	"net/http"

	"github.com/iml885203/orbit/autoupdate"
)

func (s *Server) updateAdmissionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		launchPath, err := autoupdate.LaunchPath()
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		state, err := autoupdate.Load(launchPath)
		if err == nil && state.Transaction != nil && state.Transaction.FinishedAt == nil &&
			r.Header.Get(UpdateTransactionHeader) == state.Transaction.ID {
			next.ServeHTTP(w, r)
			return
		}
		s.updateAdmissionMu.RLock()
		defer s.updateAdmissionMu.RUnlock()
		state, err = autoupdate.Load(launchPath)
		if err != nil || state.Transaction == nil || state.Transaction.FinishedAt != nil {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusConflict, APIResponse{
			Error: "Orbit is finishing a verified update; retry the mutation after it completes",
			Code:  "update_in_progress",
		})
	})
}
