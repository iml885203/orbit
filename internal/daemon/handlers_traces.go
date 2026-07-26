package daemon

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// registerTracingHandlers wires the trace query endpoints. The OTLP receiver
// itself runs on its own listener (see ListenAndServe); these are the
// dashboard/CLI-facing read APIs on the main socket + dashboard port.
func registerTracingHandlers(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("/api/tracing/status", s.handleTracingStatus)
	mux.HandleFunc("/api/traces", s.handleTracesList)
	mux.HandleFunc("/api/traces/", s.handleTraceDetail)
}

// handleTracingStatus reports collector health for the live indicator.
// Configured and the desired port come from config; ReceiverHealthy,
// ActualPort, and the counters come from the store (the store learned the real
// bind outcome via SetReceiver). Keeping the two apart lets the CLI and
// dashboard distinguish "on but the receiver never bound" from "healthy".
func (s *Server) handleTracingStatus(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	st := s.tracing.Stats()
	cfg := s.holder.Load()
	st.Configured = cfg.TracingEnabled()
	st.OTLPPort = cfg.TracingOTLPPort()
	writeJSON(w, http.StatusOK, st)
}

// handleTracesList returns trace summaries newest-first. ?limit=N caps the
// count (0 = all). The default of 100 is the API middle ground between the
// CLI's one-screenful (50) and the dashboard's scrollable list (200) — those
// surfaces pass their own explicit limits.
func (s *Server) handleTracesList(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, s.tracing.List(limit))
}

// handleTraceDetail serves /api/traces/{id} (full trace) and
// /api/traces/{id}/logs (the trace's log lines), both 404 if unknown/evicted.
func (s *Server) handleTraceDetail(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/traces/")
	id, sub, _ := strings.Cut(path, "/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "trace id required"})
		return
	}
	trace, ok := s.tracing.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIResponse{Error: "trace not found (unknown or evicted from the ring buffer)"})
		return
	}
	switch sub {
	case "":
		writeJSON(w, http.StatusOK, trace)
	case "logs":
		writeJSON(w, http.StatusOK, TraceLogsResponse{Lines: s.traceLogs(id, trace.Services)})
	default:
		writeJSON(w, http.StatusNotFound, APIResponse{Error: "unknown trace sub-resource: " + sub})
	}
}

// traceLogs joins a trace to its log lines: for each service the trace
// touched, scan that service's log ring buffer for lines carrying the trace
// id. The daemon owns both stores, so this is the single implementation of
// the join — the CLI and dashboard both consume it.
//
// The match is field-anchored (TraceId=<id> / "TraceId":"<id>" shapes), the
// same definition LogPanel's trace chips use client-side — never a bare
// substring or timestamp guess.
//
// Ceiling: bounded by the per-service log ring buffer (last N lines), not the
// trace's lifetime — a trace still in the trace buffer can have had its log
// lines age out, so an empty result can mean "aged out", not "none".
func (s *Server) traceLogs(traceID string, services []string) []TraceLogLine {
	re, err := traceIDLineRegexp(traceID)
	if err != nil {
		return nil
	}
	lines := []TraceLogLine{}
	for _, svc := range services {
		buf := s.app.Logs.GetBuffer(svc)
		if buf == nil {
			continue
		}
		for _, line := range buf.Lines() {
			if re.MatchString(line) {
				lines = append(lines, TraceLogLine{Service: svc, Line: line})
			}
		}
	}
	return lines
}

// traceIDLineRegexp matches a log line that carries traceID in one of the
// logger field shapes: `TraceId=<id>` / `"TraceId":"<id>"` (also trace_id /
// traceId casings) — mirroring ui/src/lib/traceColor.ts extractTraceId.
func traceIDLineRegexp(traceID string) (*regexp.Regexp, error) {
	id := regexp.QuoteMeta(traceID)
	return regexp.Compile(`"(?:TraceId|trace_id|traceId)"\s*:\s*"` + id + `"|\b(?:TraceId|trace_id|traceId)=` + id + `\b`)
}
