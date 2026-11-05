package daemonsrv

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
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "resource name required"})
		return
	}
	if !s.holder.Load().ServiceOrContainerExists(name) {
		writeUnknownResource(w, name)
		return
	}

	stream := len(parts) == 2 && parts[1] == "stream"
	fallback := 100
	if stream {
		fallback = 0
	}
	lines, err := requestedLogLines(r, fallback)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: err.Error()})
		return
	}

	if stream {
		s.handleLogStream(w, r, name, lines)
		return
	}

	logLines := s.app.Logs.GetLastLines(name, lines)
	writeJSON(w, http.StatusOK, LogsResponse{Lines: logLines})
}

func requestedLogLines(r *http.Request, fallback int) (int, error) {
	var lines int
	if n, err := fmt.Sscanf(r.URL.Query().Get("lines"), "%d", &lines); n == 0 || err != nil {
		return fallback, nil
	}
	if lines < 0 {
		return 0, fmt.Errorf("lines must be zero or more, got %d", lines)
	}
	return lines, nil
}

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request, name string, backlogLines int) {
	sse, err := openSSE(w)
	if err != nil {
		return
	}

	ch := make(chan string, 256)
	backlog, unsub := s.app.Logs.Follow(name, backlogLines, func(line string) {
		select {
		case ch <- line:
		default:
		}
	})
	defer unsub()

	// The backlog bypasses ch: it can hold the whole ring buffer, which
	// would overflow ch and be dropped.
	for _, line := range backlog {
		if err := sse.Send([]byte(line)); err != nil {
			slog.Error("sse write error", "component", "logs", "service", name, "err", err)
			return
		}
	}

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
