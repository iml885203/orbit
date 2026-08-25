package health

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

type httpProtocol uint8

const (
	protocolH2C httpProtocol = iota + 1
	protocolHTTP1
)

type protocolFailure struct {
	protocol string
	err      error
}

func (e protocolFailure) Error() string { return fmt.Sprintf("%s: %v", e.protocol, e.err) }
func (e protocolFailure) Unwrap() error { return e.err }

type protocolFailures struct {
	h2c   error
	http1 error
}

func (e protocolFailures) Error() string {
	return fmt.Sprintf("h2c: %v; HTTP/1.1: %v", e.h2c, e.http1)
}

type protocolDiscovery struct {
	done      chan struct{}
	response  *http.Response
	err       error
	abandoned bool
}

type protocolTransport struct {
	ctx      context.Context
	timeout  time.Duration
	http1    http.RoundTripper
	h2c      http.RoundTripper
	https    http.RoundTripper
	mu       sync.Mutex
	selected map[string]httpProtocol
	inflight map[string]*protocolDiscovery
	closed   bool
}

func newProtocolTransport(ctx context.Context, https http.RoundTripper, timeout time.Duration) *protocolTransport {
	http1 := http.DefaultTransport.(*http.Transport).Clone()
	http1.ForceAttemptHTTP2 = false
	return &protocolTransport{
		ctx:     ctx,
		timeout: timeout,
		http1:   http1,
		h2c: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
		https:    https,
		selected: make(map[string]httpProtocol),
		inflight: make(map[string]*protocolDiscovery),
	}
}

func (t *protocolTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	if req.URL.Scheme == "https" {
		return t.https.RoundTrip(req)
	}
	origin := requestOrigin(req.URL)
	for {
		if err := t.ctx.Err(); err != nil {
			return nil, err
		}
		t.mu.Lock()
		protocol := t.selected[origin]
		if protocol != 0 {
			t.mu.Unlock()
			resp, firstErr := t.roundTrip(protocol, req)
			if firstErr == nil {
				return resp, nil
			}
			if req.Context().Err() != nil {
				return nil, req.Context().Err()
			}
			alternate := protocolH2C
			if protocol == protocolH2C {
				alternate = protocolHTTP1
			}
			resp, alternateErr := t.roundTrip(alternate, req)
			if alternateErr != nil {
				if protocol == protocolH2C {
					return nil, protocolFailures{h2c: firstErr, http1: alternateErr}
				}
				return nil, protocolFailures{h2c: alternateErr, http1: firstErr}
			}
			t.mu.Lock()
			if t.selected[origin] == protocol {
				t.selected[origin] = alternate
			}
			t.mu.Unlock()
			return resp, nil
		}
		if discovery := t.inflight[origin]; discovery != nil {
			t.mu.Unlock()
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-discovery.done:
				continue
			}
		}

		discovery := &protocolDiscovery{done: make(chan struct{})}
		t.inflight[origin] = discovery
		t.mu.Unlock()
		go t.discover(origin, req, discovery)

		select {
		case <-req.Context().Done():
			t.abandon(origin, discovery)
			return nil, req.Context().Err()
		case <-discovery.done:
			return discovery.response, discovery.err
		}
	}
}

func (t *protocolTransport) discover(origin string, req *http.Request, discovery *protocolDiscovery) {
	ctx := t.ctx
	var cancel context.CancelFunc
	if deadline, ok := req.Context().Deadline(); ok {
		ctx, cancel = context.WithDeadline(t.ctx, deadline)
	} else {
		ctx, cancel = context.WithTimeout(t.ctx, t.timeout)
	}
	defer cancel()
	probe := req.Clone(ctx)
	resp, h2cErr := t.h2c.RoundTrip(probe)
	protocol := protocolH2C
	var err error
	if h2cErr != nil && ctx.Err() == nil {
		probe = req.Clone(ctx)
		resp, err = t.http1.RoundTrip(probe)
		protocol = protocolHTTP1
		if err != nil {
			err = protocolFailures{h2c: h2cErr, http1: err}
		}
	} else if h2cErr != nil {
		err = protocolFailures{h2c: h2cErr, http1: ctx.Err()}
	}
	t.mu.Lock()
	if t.closed || t.ctx.Err() != nil {
		if resp != nil {
			_ = resp.Body.Close()
			resp = nil
		}
		err = t.ctx.Err()
	} else if err == nil {
		t.selected[origin] = protocol
	}
	delete(t.inflight, origin)
	discovery.response = resp
	discovery.err = err
	abandoned := discovery.abandoned
	close(discovery.done)
	t.mu.Unlock()
	if abandoned && resp != nil {
		_ = resp.Body.Close()
	}
}

func (t *protocolTransport) close() {
	t.mu.Lock()
	t.closed = true
	clear(t.selected)
	t.mu.Unlock()
	if transport, ok := t.http1.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	if transport, ok := t.h2c.(*http2.Transport); ok {
		transport.CloseIdleConnections()
	}
}

func (t *protocolTransport) abandon(origin string, discovery *protocolDiscovery) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inflight[origin] == discovery {
		discovery.abandoned = true
		return
	}
	if discovery.response != nil {
		_ = discovery.response.Body.Close()
		discovery.response = nil
	}
}

func (t *protocolTransport) roundTrip(protocol httpProtocol, req *http.Request) (*http.Response, error) {
	if protocol == protocolH2C {
		resp, err := t.h2c.RoundTrip(req)
		if err != nil {
			return nil, protocolFailure{protocol: "h2c", err: err}
		}
		return resp, nil
	}
	resp, err := t.http1.RoundTrip(req)
	if err != nil {
		return nil, protocolFailure{protocol: "HTTP/1.1", err: err}
	}
	return resp, nil
}

func requestOrigin(u *url.URL) string {
	return u.Scheme + "://" + u.Host
}
