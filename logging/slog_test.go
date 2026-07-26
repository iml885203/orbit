package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func newTestLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	return slog.New(NewOrbitHandler(buf, level))
}

func TestOrbitHandler_InfoWithComponent(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, slog.LevelInfo)
	log.Info("starting foo", "component", "orbit")

	got := strings.TrimRight(buf.String(), "\n")
	want := "[orbit] starting foo"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOrbitHandler_WarnPrefix(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, slog.LevelInfo)
	log.Warn("port still held", "component", "process", "port", 8080)

	got := strings.TrimRight(buf.String(), "\n")
	want := "[process] warning: port still held port=8080"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOrbitHandler_ErrorWithErrAttr(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, slog.LevelInfo)
	log.Error("reconnect failed", "component", "orbit", "name", "redis", "err", errors.New("pid 42 gone"))

	got := strings.TrimRight(buf.String(), "\n")
	want := `[orbit] error: reconnect failed name=redis err="pid 42 gone"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOrbitHandler_NoComponent(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, slog.LevelInfo)
	log.Info("bare message")

	got := strings.TrimRight(buf.String(), "\n")
	want := "bare message"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOrbitHandler_LevelFilter(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, slog.LevelWarn)
	log.Info("invisible", "component", "orbit")
	log.Warn("visible", "component", "orbit")

	got := buf.String()
	if strings.Contains(got, "invisible") {
		t.Errorf("Info leaked through Warn filter: %q", got)
	}
	if !strings.Contains(got, "[orbit] warning: visible") {
		t.Errorf("Warn missing: %q", got)
	}
}

func TestOrbitHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(NewOrbitHandler(&buf, slog.LevelInfo))
	log := base.With("component", "health", "service", "redis")
	log.Info("healthy")

	got := strings.TrimRight(buf.String(), "\n")
	want := "[health] healthy service=redis"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"":      slog.LevelInfo,
		"debug": slog.LevelDebug,
		"INFO":  slog.LevelInfo,
		"Warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"junk":  slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
