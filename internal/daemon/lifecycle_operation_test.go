package daemon

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type lifecycleRoundTripper func(*http.Request) (*http.Response, error)

func (f lifecycleRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLifecycleClientPinsAcceptedMutationAndReadsExplicitResult(t *testing.T) {
	mutations := 0
	c := &Client{ctx: context.Background(), http: &http.Client{Transport: lifecycleRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := `{"id":"op-1","kind":"stop","state":"failed","phase":"completed","epoch":123,"generation":4,"config_path":"project/orbit.yaml","resources":["api"],"error":"controlled failure","code":"lifecycle_failed"}`
		if r.Method == http.MethodPost {
			mutations++
			if r.Header.Get(ExpectedEpochHeader) != "123" || r.Header.Get(ExpectedGenerationHeader) != "4" {
				t.Errorf("unpinned mutation headers=%v", r.Header)
			}
			body = `{"ok":true,"operation_id":"op-1","affected_resources":["api"]}`
		} else if r.URL.Path != "/api/operations/op-1" {
			t.Errorf("operation path=%s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pinned := c.WithEnvironment(123, 4).WithContext(ctx)
	accepted, err := pinned.Down(false)
	if err != nil || accepted.OperationID != "op-1" {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
	operation, err := pinned.Operation(accepted.OperationID)
	if err != nil || operation.State != "failed" || operation.Code != "lifecycle_failed" || operation.Generation != 4 {
		t.Fatalf("operation=%+v err=%v", operation, err)
	}
	if mutations != 1 {
		t.Fatalf("mutation count=%d", mutations)
	}
}
