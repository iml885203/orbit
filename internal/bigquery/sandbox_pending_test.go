package bigquery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	bq "google.golang.org/api/bigquery/v2"
	"google.golang.org/api/option"
)

type pendingObservationContext struct{ context.Context }

func (c pendingObservationContext) Err() error { return context.Cause(c.Context) }

type pendingJobTransport struct {
	base           http.RoundTripper
	cancel         context.CancelCauseFunc
	cause          error
	setupEntered   chan struct{}
	releaseSetup   chan struct{}
	responseClosed bool
}

func (p *pendingJobTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if p.setupEntered != nil {
		close(p.setupEntered)
		p.setupEntered = nil
		select {
		case <-p.releaseSetup:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	response, err := p.base.RoundTrip(req)
	if err == nil && response.StatusCode == http.StatusOK && req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/jobs") {
		response.Body = pendingJobResponse{ReadCloser: response.Body, transport: p}
	}
	return response, err
}

type pendingJobResponse struct {
	io.ReadCloser
	transport *pendingJobTransport
}

func (b pendingJobResponse) Close() error {
	err := b.ReadCloser.Close()
	b.transport.responseClosed = true
	b.transport.cancel(b.transport.cause)
	return err
}

func TestJobPendingObservationPreservesEvidence(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			fake := testFake(t)
			fake.pending = true
			r := testRunner(t, fake, t.TempDir())
			watchdog, stop := context.WithTimeout(t.Context(), 10*time.Second)
			defer stop()
			parent, cancel := context.WithCancelCause(watchdog)
			defer cancel(context.Canceled)
			ctx := pendingObservationContext{parent}
			entered, release := make(chan struct{}), make(chan struct{})
			base := http.DefaultTransport.(*http.Transport).Clone()
			base.Proxy = nil
			defer base.CloseIdleConnections()
			transport := &pendingJobTransport{base: base, cancel: cancel, cause: cause, setupEntered: entered, releaseSetup: release}
			endpoint, err := url.Parse(fake.server.URL)
			if err != nil {
				t.Fatal(err)
			}
			api, err := bq.NewService(context.Background(), option.WithHTTPClient(&http.Client{Transport: localTransport{host: endpoint.Host, base: transport}}), option.WithEndpoint(fake.server.URL+"/bigquery/v2/"))
			if err != nil {
				t.Fatal(err)
			}
			r.api = api
			type outcome struct {
				rec *Receipt
				err error
			}
			result := make(chan outcome, 1)
			go func() { rec, err := r.prepare(ctx, testProfile(), "run-1", testInputs()); result <- outcome{rec, err} }()
			select {
			case <-entered:
				if ctx.Err() != nil {
					t.Fatalf("setup consumed observation deadline: %v", ctx.Err())
				}
				close(release)
			case <-watchdog.Done():
				t.Fatal("setup did not reach controlled boundary")
			}
			var got outcome
			select {
			case got = <-result:
			case <-watchdog.Done():
				t.Fatal("pending observation did not terminate")
			}
			if watchdog.Err() != nil {
				t.Fatal("watchdog expired before the controlled observation completed")
			}
			if !transport.responseClosed {
				t.Fatal("observation ended before the SDK finished the accepted reply")
			}
			requireCode(t, got.err, "job_pending")
			if !errors.Is(got.err, cause) {
				t.Fatalf("lost observation cause: %v", got.err)
			}
			saved, err := r.store.read(got.rec.Scope)
			if err != nil {
				t.Fatal(err)
			}
			if saved.DataReady || saved.Jobs["orders"] == "" || saved.Jobs["orders"] != got.rec.Jobs["orders"] || saved.JobStates["orders"] != "submission_intent" {
				t.Fatalf("lost pending evidence: %+v", saved)
			}
			_, err = r.clean(context.Background(), testProfile(), "run-1", true)
			requireCode(t, err, "cleanup_job_pending")
			loads, deletes := fake.counts()
			if loads != 1 || deletes != 0 {
				t.Fatalf("pending job side effects: loads=%d deletes=%d", loads, deletes)
			}
		})
	}
}
