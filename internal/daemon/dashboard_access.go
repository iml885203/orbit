package daemon

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// dashboardAccessMiddleware protects the browser-reachable TCP API from DNS
// rebinding and cross-site mutations. Unix-socket requests are local process
// traffic and bypass these browser-specific checks.
func dashboardAccessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requestUsesTCP(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}
		if !loopbackHost(r.Host) {
			http.Error(w, "dashboard requests must use a loopback host", http.StatusForbidden)
			return
		}
		if mutationRequest(r) && !sameOriginDashboardRequest(r) {
			http.Error(w, "cross-site dashboard mutation blocked", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestUsesTCP(ctx context.Context) bool {
	switch ctx.Value(http.LocalAddrContextKey).(type) {
	case *net.TCPAddr:
		return true
	default:
		return false
	}
}

func loopbackHost(hostport string) bool {
	host := hostport
	if parsed, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsed
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

func mutationRequest(r *http.Request) bool {
	return r.Method != http.MethodGet &&
		r.Method != http.MethodHead &&
		r.Method != http.MethodOptions
}

func sameOriginDashboardRequest(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host) && loopbackHost(parsed.Host)
}
