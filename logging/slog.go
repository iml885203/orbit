package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// SetupDefault installs an OrbitHandler as the slog default, writing to w at
// the level named by the given env var (defaults to Info when empty/unknown).
func SetupDefault(w io.Writer, levelEnvVar string) {
	slog.SetDefault(slog.New(NewOrbitHandler(w, ParseLevel(os.Getenv(levelEnvVar)))))
}

// OrbitHandler is a slog.Handler that produces "[component] msg key=value"
// output, preserving the human-readable shape Orbit's log.Printf convention
// already establishes. Warn and Error levels prepend "warning: " / "error: ".
type OrbitHandler struct {
	w     io.Writer
	mu    *sync.Mutex
	level slog.Level
	attrs []slog.Attr
}

// NewOrbitHandler builds a handler writing to w at the given minimum level.
func NewOrbitHandler(w io.Writer, level slog.Level) *OrbitHandler {
	return &OrbitHandler{w: w, mu: &sync.Mutex{}, level: level}
}

func (h *OrbitHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *OrbitHandler) Handle(_ context.Context, r slog.Record) error {
	var component string
	var kvParts []string

	render := func(a slog.Attr) {
		if a.Key == "component" {
			component = a.Value.String()
			return
		}
		kvParts = append(kvParts, formatKV(a))
	}
	for _, a := range h.attrs {
		render(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		render(a)
		return true
	})

	var b strings.Builder
	if component != "" {
		b.WriteByte('[')
		b.WriteString(component)
		b.WriteString("] ")
	}
	switch {
	case r.Level >= slog.LevelError:
		b.WriteString("error: ")
	case r.Level >= slog.LevelWarn:
		b.WriteString("warning: ")
	}
	b.WriteString(r.Message)
	for _, kv := range kvParts {
		b.WriteByte(' ')
		b.WriteString(kv)
	}
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *OrbitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &OrbitHandler{w: h.w, mu: h.mu, level: h.level, attrs: merged}
}

func (h *OrbitHandler) WithGroup(_ string) slog.Handler {
	return h
}

func formatKV(a slog.Attr) string {
	key := a.Key
	v := a.Value.Resolve()
	s := v.String()
	if needsQuoting(s) {
		return fmt.Sprintf("%s=%q", key, s)
	}
	return key + "=" + s
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r == ' ' || r == '"' || r == '=' || r < 0x20 {
			return true
		}
	}
	return false
}

// ParseLevel maps a case-insensitive name to slog.Level. Unknown or empty
// strings fall back to Info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
