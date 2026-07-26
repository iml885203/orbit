package daemon

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/logs/")
	parts := strings.SplitN(path, "/", 2)
	name := parts[0]

	if name == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "service name required"})
		return
	}

	// Check for SSE stream
	if len(parts) == 2 && parts[1] == "stream" {
		s.handleLogStream(w, r, name)
		return
	}

	if _, ok := s.app.Orchestrator.GetServiceInfo(name); !ok {
		writeJSON(w, http.StatusNotFound, APIResponse{Error: fmt.Sprintf("unknown service: %s", name)})
		return
	}

	// Return last N lines
	lines := 100
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, err := fmt.Sscanf(q, "%d", &lines); n == 0 || err != nil {
			lines = 100
		}
	}

	logLines := s.app.Logs.GetLastLines(name, lines)
	writeJSON(w, http.StatusOK, LogsResponse{Lines: logLines})
}

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request, name string) {
	sse, err := openSSE(w)
	if err != nil {
		return
	}

	ch := make(chan string, 256)
	unsub := s.app.Logs.Subscribe(func(svc string, line string) {
		if svc == name {
			select {
			case ch <- line:
			default:
			}
		}
	})
	defer unsub()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case line := <-ch:
			if ctx.Err() != nil {
				return
			}
			if err := sse.Send([]byte(line)); err != nil {
				slog.Error("sse write error", "component", "logs", "service", name, "err", err)
				return
			}
		}
	}
}
