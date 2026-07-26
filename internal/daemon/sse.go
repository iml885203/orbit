package daemon

import (
	"errors"
	"fmt"
	"net/http"
)

// errSSEUnsupported is returned by openSSE when the ResponseWriter does not
// implement http.Flusher (unusual — most handlers wrap a flushing writer).
var errSSEUnsupported = errors.New("streaming not supported")

// sseWriter sends Server-Sent Event frames to a response writer.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// openSSE writes the SSE response headers and returns a writer for sending
// frames. Returns errSSEUnsupported if the ResponseWriter cannot flush.
func openSSE(w http.ResponseWriter) (*sseWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: errSSEUnsupported.Error()})
		return nil, errSSEUnsupported
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &sseWriter{w: w, flusher: flusher}, nil
}

// Send writes one SSE `data:` frame and flushes.
func (s *sseWriter) Send(data []byte) error {
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// SendEvent writes one named SSE frame (`event: <type>\ndata: <payload>`) and
// flushes. Used by the multiplexed /api/events stream so clients can dispatch
// via addEventListener('<type>', ...).
func (s *sseWriter) SendEvent(eventType string, data []byte) error {
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", eventType, data); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
