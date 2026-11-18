package bigquery

import (
	"context"
	bq "google.golang.org/api/bigquery/v2"
	"google.golang.org/api/option"
	"net/http"
	"net/url"
)

type localTransport struct {
	host string
	base http.RoundTripper
}

func (t localTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "http" || req.URL.Host != t.host || req.Header.Get("Authorization") != "" {
		return nil, failure("offline_network_blocked", nil)
	}
	return t.base.RoundTrip(req)
}

func offlineClient(endpoint string) (*bq.Service, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Scheme != "http" {
		return nil, failure("offline_endpoint_required", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Transport: localTransport{host: u.Host, base: transport}}
	return bq.NewService(context.Background(), option.WithHTTPClient(client), option.WithEndpoint(endpoint+"/bigquery/v2/"))
}
