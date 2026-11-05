package daemonsrv

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type logStream struct {
	t       *testing.T
	scanner *bufio.Scanner
}

func openLogStream(t *testing.T, s *Server, query string) *logStream {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(s.handleLogs))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/logs/api/stream"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	subscribersBefore := s.app.Logs.SubscriberCount()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.app.Logs.SubscriberCount() == subscribersBefore {
		if time.Now().After(deadline) {
			t.Fatal("stream never subscribed to the multiplexer")
		}
		time.Sleep(time.Millisecond)
	}
	return &logStream{t: t, scanner: bufio.NewScanner(resp.Body)}
}

func (l *logStream) next() string {
	l.t.Helper()
	for l.scanner.Scan() {
		if line, ok := strings.CutPrefix(l.scanner.Text(), "data: "); ok {
			return line
		}
	}
	l.t.Fatalf("stream ended: %v", l.scanner.Err())
	return ""
}

func TestLogStreamSendsRequestedBacklogBeforeLiveLines(t *testing.T) {
	s := newTestServer(t, testConfig())
	const backlog = 300 // more than the stream's 256-slot live channel
	s.app.Logs.Write("api", "evicted by the tail")
	for i := 0; i < backlog; i++ {
		s.app.Logs.Write("api", fmt.Sprintf("old %d", i))
	}
	s.app.Logs.Write("web", "other resource")

	stream := openLogStream(t, s, fmt.Sprintf("?lines=%d", backlog))
	s.app.Logs.Write("web", "other resource")
	s.app.Logs.Write("api", "live")

	for i := 0; i < backlog; i++ {
		if got, want := stream.next(), fmt.Sprintf("old %d", i); got != want {
			t.Fatalf("backlog line %d = %q, want %q", i, got, want)
		}
	}
	if got := stream.next(); got != "live" {
		t.Fatalf("line after backlog = %q, want live", got)
	}
}

func TestLogStreamWithoutBacklogStreamsOnlyLiveLines(t *testing.T) {
	for _, query := range []string{"", "?lines=0"} {
		t.Run("query="+query, func(t *testing.T) {
			s := newTestServer(t, testConfig())
			s.app.Logs.Write("api", "old")

			stream := openLogStream(t, s, query)
			s.app.Logs.Write("api", "live 1")
			s.app.Logs.Write("api", "live 2")

			for _, want := range []string{"live 1", "live 2"} {
				if got := stream.next(); got != want {
					t.Fatalf("line = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestLogsRejectsNegativeLineCount(t *testing.T) {
	for _, path := range []string{"/api/logs/api?lines=-1", "/api/logs/api/stream?lines=-1"} {
		t.Run(path, func(t *testing.T) {
			s := newTestServer(t, testConfig())
			s.app.Logs.Write("api", "line")
			rec := httptest.NewRecorder()

			s.handleLogs(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestLogsWithZeroLinesReturnsNoLines(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.app.Logs.Write("api", "line")
	rec := httptest.NewRecorder()

	s.handleLogs(rec, httptest.NewRequest(http.MethodGet, "/api/logs/api?lines=0", nil))

	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"lines":[]}` {
		t.Errorf("response = %d %s, want 200 with no lines", rec.Code, rec.Body.String())
	}
}
