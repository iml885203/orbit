package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
)

func prepareLogsCommand(t *testing.T, logs http.HandlerFunc) {
	t.Helper()
	prepareLifecycleProgressTest(t)
	if err := os.WriteFile(configFile, []byte("name: logs-test\nservices:\n  api:\n    command: echo api\n  web:\n    command: echo web\n"), 0600); err != nil {
		t.Fatal(err)
	}
	priorLines, priorFollow := logLines, follow
	t.Cleanup(func() { logLines, follow = priorLines, priorFollow })
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/status":
			daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{ConfigPath: configFile, Resources: []daemon.ResourceStatus{{Name: "api", State: "healthy"}, {Name: "web", State: "healthy"}}})
		default:
			logs(w, r)
		}
	})
}

func executeLogsCommand(ctx context.Context, out io.Writer, args ...string) error {
	cmd := logsCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(out)
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

func TestLogsMultipleSnapshotsDeduplicateAndPreserveSingleContract(t *testing.T) {
	var requests atomic.Int32
	prepareLogsCommand(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("lines") != "2" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		name := strings.TrimPrefix(r.URL.Path, "/api/logs/")
		daemon.WriteJSON(w, http.StatusOK, daemon.LogsResponse{Lines: []string{name + " history", name + " recent"}})
	})
	var out bytes.Buffer
	if err := executeLogsCommand(context.Background(), &out, "api", "web", "api", "--lines", "2"); err != nil {
		t.Fatal(err)
	}
	assertOneLifecycleEnvelope(t, out.Bytes(), true)
	data := envelopeData(t, out.Bytes())
	resources := data["resources"].([]any)
	if requests.Load() != 2 || len(resources) != 2 {
		t.Fatalf("requests=%d data=%s", requests.Load(), out.String())
	}
	for i, name := range []string{"api", "web"} {
		resource := resources[i].(map[string]any)
		if resource["resource"] != name || resource["lines_requested"] != float64(2) || resource["truncated"] != true || resource["lines"].([]any)[0] != name+" history" {
			t.Fatalf("resource=%v", resource)
		}
	}
	out.Reset()
	if err := executeLogsCommand(context.Background(), &out, "api", "--tail", "2"); err != nil {
		t.Fatal(err)
	}
	if data = envelopeData(t, out.Bytes()); data["resource"] != "api" || data["resources"] != nil {
		t.Fatalf("single data=%v", data)
	}
}

func TestLogsValidateEveryTargetBeforeOutputOrSubscription(t *testing.T) {
	var requests atomic.Int32
	prepareLogsCommand(t, func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.NotFound(w, r) })
	for _, args := range [][]string{{"api", "missing"}, {"api", "missing", "--follow"}} {
		var out bytes.Buffer
		err := executeLogsCommand(context.Background(), &out, args...)
		if err == nil || out.Len() != 0 || requests.Load() != 0 {
			t.Fatalf("args=%v err=%v output=%s requests=%d", args, err, out.String(), requests.Load())
		}
	}
}

func TestLogsHumanMultipleSnapshotAttributesEveryLine(t *testing.T) {
	prepareLogsCommand(t, func(w http.ResponseWriter, r *http.Request) {
		daemon.WriteJSON(w, http.StatusOK, daemon.LogsResponse{Lines: []string{"one\ntwo"}})
	})
	cli.JSONOutput = false
	var out bytes.Buffer
	if err := executeLogsCommand(context.Background(), &out, "api", "web"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[api] one\n[api] two\n[web] one\n[web] two\n" {
		t.Fatalf("output=%q", out.String())
	}
}

func TestLogsFollowSerializesHistoryAndLiveAndCancelsQuietPeers(t *testing.T) {
	var started sync.WaitGroup
	started.Add(2)
	var completed atomic.Int32
	prepareLogsCommand(t, func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/logs/"), "/stream")
		if r.URL.Query().Get("lines") != "2" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		started.Done()
		started.Wait()
		for _, line := range []string{"history", "live"} {
			_, _ = fmt.Fprintf(w, "data: %s %s\n\n", name, line)
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
		completed.Add(1)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out := &cancelAfterLogWriter{cancel: cancel, count: 4}
	if err := executeLogsCommand(ctx, out, "api", "web", "api", "--follow", "--lines", "2"); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
	byResource := map[string][]string{}
	for decoder.More() {
		var event logJSONEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "log" {
			t.Fatalf("event=%+v", event)
		}
		byResource[event.Resource] = append(byResource[event.Resource], event.Line)
	}
	for _, name := range []string{"api", "web"} {
		if fmt.Sprint(byResource[name]) != fmt.Sprint([]string{name + " history", name + " live"}) {
			t.Fatalf("events=%v", byResource)
		}
	}
	deadline := time.After(time.Second)
	for completed.Load() != 2 {
		select {
		case <-deadline:
			t.Fatalf("closed subscriptions=%d", completed.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

type cancelAfterLogWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
	count  int
}

func (w *cancelAfterLogWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	w.count--
	if w.count == 0 {
		w.cancel()
	}
	return n, err
}

func TestLogsFollowFailureCancelsQuietPeerAndEmitsAttributedError(t *testing.T) {
	peerStarted := make(chan struct{})
	peerClosed := make(chan struct{})
	prepareLogsCommand(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/web/") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			close(peerStarted)
			<-r.Context().Done()
			close(peerClosed)
			return
		}
		<-peerStarted
		http.Error(w, "failed resource", http.StatusInternalServerError)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	err := executeLogsCommand(ctx, &out, "api", "web", "--follow")
	var rendered errCLIJSONAlreadyRendered
	if !errors.As(err, &rendered) {
		t.Fatalf("err=%v", err)
	}
	var event logJSONErrorEvent
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatalf("%v output=%s", err, out.String())
	}
	if event.Type != "error" || event.Resource != "api" || event.Error.Code != "log_stream_error" {
		t.Fatalf("event=%+v", event)
	}
	select {
	case <-peerClosed:
	case <-ctx.Done():
		t.Fatal("quiet peer not closed")
	}
}

func TestLogsFollowBrokenWriterCancelsAllSubscriptions(t *testing.T) {
	var started sync.WaitGroup
	started.Add(2)
	var closed sync.WaitGroup
	closed.Add(2)
	prepareLogsCommand(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		started.Done()
		started.Wait()
		_, _ = fmt.Fprint(w, "data: hello\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		closed.Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := executeLogsCommand(ctx, closedLogWriter{}, "api", "web", "--follow")
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("error=%v", err)
	}
	done := make(chan struct{})
	go func() { closed.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("subscriptions not closed")
	}
}

type closedLogWriter struct{}

func (closedLogWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestLogsFollowCleanEndKeepsOtherResourceFollowing(t *testing.T) {
	apiEnded := make(chan struct{})
	prepareLogsCommand(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(r.URL.Path, "/api/logs/api/") {
			_, _ = fmt.Fprint(w, "data: api history\n\n")
			close(apiEnded)
			return
		}
		<-apiEnded
		_, _ = fmt.Fprint(w, "data: web live\n\n")
	})
	var out bytes.Buffer
	if err := executeLogsCommand(context.Background(), &out, "api", "web", "--follow"); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
	found := map[string]bool{}
	for decoder.More() {
		var event logJSONEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		found[event.Resource] = event.Type == "log"
	}
	if !found["api"] || !found["web"] {
		t.Fatalf("output=%s", out.String())
	}
}
