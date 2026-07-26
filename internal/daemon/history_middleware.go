package daemon

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/iml885203/orbit/internal/cmdmap"
	"github.com/iml885203/orbit/internal/cmdmap/gaps"
	"github.com/iml885203/orbit/internal/history"
)

const maxHistoryBodyBytes = 256 * 1024

func HistoryMiddleware(rec *history.Recorder, gc *gaps.Collector) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rec == nil {
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get(cliOriginHeader) == "cli" {
				next.ServeHTTP(w, r)
				return
			}
			entry := cmdmap.Resolve(r.Method, r.URL.Path, nil)
			if !entry.UserAction {
				next.ServeHTTP(w, r)
				return
			}
			var body []byte
			if r.Body != nil {
				var err error
				body, err = io.ReadAll(io.LimitReader(r.Body, maxHistoryBodyBytes+1))
				if err != nil {
					writeJSON(w, http.StatusBadRequest, APIResponse{Error: "reading request body"})
					return
				}
				if len(body) > maxHistoryBodyBytes {
					writeJSON(w, http.StatusRequestEntityTooLarge, APIResponse{Error: "request body too large"})
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
			}
			entry = cmdmap.Resolve(r.Method, r.URL.Path, body)

			id := history.NewID()
			start := time.Now()
			rec.Record(history.Record{
				ID:        id,
				Timestamp: start,
				Source:    history.SourceUI,
				Method:    r.Method,
				Path:      r.URL.Path,
				Command:   entry.Command,
				Summary:   entry.Summary,
				HasCLI:    entry.HasCLI,
				Status:    history.StatusPending,
			})
			if !entry.HasCLI && gc != nil {
				gc.Track(r.Method, entry.PathPattern, entry.Summary)
				_ = gc.Flush()
			}

			cw := &captureWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(cw, r)
			status := history.StatusOK
			errMsg := ""
			if cw.status >= 400 {
				status = history.StatusError
				errMsg = http.StatusText(cw.status)
			}
			rec.Record(history.Record{
				ID:         id,
				Timestamp:  time.Now(),
				Source:     history.SourceUI,
				Method:     r.Method,
				Path:       r.URL.Path,
				Command:    entry.Command,
				Summary:    entry.Summary,
				HasCLI:     entry.HasCLI,
				Status:     status,
				DurationMs: time.Since(start).Milliseconds(),
				Error:      errMsg,
			})
		})
	}
}

type captureWriter struct {
	http.ResponseWriter
	status int
}

func (w *captureWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
