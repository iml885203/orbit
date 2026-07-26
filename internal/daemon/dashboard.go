package daemon

import "net/http"

// staticHandler serves the dashboard assets injected via NewServer. The
// embed lives in each binary's main package (the built dist is a
// property of the distribution, like its extension panels) — the core
// server only serves whatever fs.FS it was handed. A nil FS (e.g. a
// core build without a compiled dashboard) serves an explanatory page
// instead of a confusing 404.
func (s *Server) staticHandler() http.Handler {
	if s.staticFS == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no dashboard embedded in this build — the API is still available under /api/", http.StatusNotFound)
		})
	}
	fileServer := http.FileServer(http.FS(s.staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		fileServer.ServeHTTP(w, r)
	})
}
