package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/tunlease/pkg/tunnelcli"
	"github.com/iml885203/tunlease/pkg/tunnelclient"
)

const (
	statusConnecting = "connecting"
	statusHealthy    = "healthy"
	statusError      = "error"
)

// Tunnel is the state for one local port. Field access requires the
// owning TunnelManager.mu — except inside a claim/release operation,
// where holding opMu alone is sufficient for reads: opMu serializes
// whole operations, so no other writer can run concurrently.
type Tunnel struct {
	LocalPort, ProxyPort int
	Paths                []string
	Status, LastError    string
	stopProxy            func()
	// One session per path keeps release-path independent: batching paths into
	// one Tunlease session would require interrupting every sibling to release one.
	sessions map[string]*tunnelclient.Session
}

// TunnelManager owns every tunnel. Lock order: opMu before mu, never
// the reverse.
type TunnelManager struct {
	// opMu serializes whole claim/release operations end to end —
	// including session start/close network I/O, which must happen
	// outside mu so status reads never block on the gateway.
	opMu sync.Mutex
	// mu guards tunnels and all Tunnel fields for readers outside an
	// operation (views, watch); see the Tunnel comment for the
	// opMu-only read exception.
	mu        sync.RWMutex
	tunnels   map[int]*Tunnel
	host      extension.Host
	accessHub *AccessLogHub
}

func NewTunnelManager(host extension.Host, hub *AccessLogHub) *TunnelManager {
	return &TunnelManager{tunnels: map[int]*Tunnel{}, host: host, accessHub: hub}
}
func (tm *TunnelManager) client(overrides *ClientAPIOptions) (*tunnelclient.Client, error) {
	c := ClaimFrom(tm.host.Config())
	config := tunnelclient.Config{}
	if c != nil {
		config.Gateway, config.Token, config.Insecure = c.Gateway, c.Token, c.Insecure
	}
	if overrides != nil {
		if overrides.Gateway != "" {
			config.Gateway = overrides.Gateway
		}
		if overrides.Token != "" {
			config.Token = overrides.Token
		}
		config.Insecure = config.Insecure || overrides.Insecure
	}
	if config.Gateway == "" {
		return nil, fmt.Errorf("no Tunlease gateway configured for this env")
	}
	return tunnelclient.New(config)
}

func (tm *TunnelManager) claim(req ClaimAPIRequest) error {
	tm.opMu.Lock()
	defer tm.opMu.Unlock()

	options, err := (tunnelcli.ClaimFlags{
		To: req.LocalPort, Gateway: req.Gateway, Token: req.Token, Insecure: req.Insecure,
	}).Options(req.Paths)
	if err != nil {
		return err
	}
	client, err := tm.client(&ClientAPIOptions{
		Gateway: req.Gateway, Token: req.Token, Insecure: req.Insecure,
	})
	if err != nil {
		return err
	}
	tunnel, pending, dead := tm.prepareClaim(options.To, options.Paths)
	// Close dead sessions here — outside mu (Close blocks on gateway
	// I/O), serialized by the opMu we hold.
	for _, s := range dead {
		_ = s.Close()
	}
	if len(pending) == 0 {
		return nil
	}
	if err := tm.ensureProxy(tunnel); err != nil {
		return tm.fail(options.To, err)
	}
	return tm.startSessions(client, tunnel, pending)
}

func (tm *TunnelManager) prepareClaim(port int, paths []string) (*Tunnel, []string, []*tunnelclient.Session) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t := tm.tunnels[port]
	if t == nil {
		t = &Tunnel{LocalPort: port, Status: statusConnecting, sessions: map[string]*tunnelclient.Session{}}
		tm.tunnels[port] = t
	}
	var pending []string
	var dead []*tunnelclient.Session
	for _, path := range paths {
		// A dead session (watch recorded its error) must not satisfy the
		// claim — drop it so the path reconnects instead of silently
		// no-oping until an explicit release. Closing is the caller's
		// job: Close blocks on gateway I/O and must not run under mu.
		if s := t.sessions[path]; s != nil && s.Err() != nil {
			dead = append(dead, s)
			delete(t.sessions, path)
		}
		if t.sessions[path] == nil {
			pending = append(pending, path)
		}
	}
	return t, pending, dead
}

func (tm *TunnelManager) ensureProxy(t *Tunnel) error {
	if t.stopProxy != nil {
		return nil
	}
	proxy, stop, err := startAccessLogProxy(t.LocalPort, tm.accessHub)
	if err != nil {
		return err
	}
	tm.mu.Lock()
	t.ProxyPort, t.stopProxy = proxy, stop
	tm.mu.Unlock()
	return nil
}

func (tm *TunnelManager) startSessions(client *tunnelclient.Client, t *Tunnel, paths []string) error {
	started := map[string]*tunnelclient.Session{}
	// Deliberately use one-path claims so adding or releasing a path never
	// reconnects the other paths sharing this local port.
	for _, path := range paths {
		session, err := startSession(client, path, t.ProxyPort)
		if err != nil {
			for _, session := range started {
				_ = session.Close()
			}
			if len(t.sessions) == 0 {
				closeTunnel(tm.detach(t.LocalPort))
			}
			// Surviving sessions keep whatever status watch() last
			// recorded — a failed claim for a NEW path is no evidence
			// the existing ones are healthy.
			return fmt.Errorf("claim %s: %w", path, err)
		}
		started[path] = session
	}
	tm.mu.Lock()
	for p, session := range started {
		t.sessions[p] = session
	}
	t.rebuildPaths()
	tm.mu.Unlock()
	tm.setStatus(t.LocalPort, statusHealthy, "")
	for p, session := range started {
		go tm.watch(t.LocalPort, p, session)
	}
	return nil
}

func startSession(client *tunnelclient.Client, path string, proxyPort int) (*tunnelclient.Session, error) {
	return client.Start(context.Background(), []string{path}, proxyPort)
}

// setStatus is the one owner of the guarded Status/LastError write.
func (tm *TunnelManager) setStatus(port int, status, errMsg string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if t := tm.tunnels[port]; t != nil {
		t.Status, t.LastError = status, errMsg
	}
}
func (t *Tunnel) rebuildPaths() {
	t.Paths = t.Paths[:0]
	for p := range t.sessions {
		t.Paths = append(t.Paths, p)
	}
	sort.Strings(t.Paths)
}
func (tm *TunnelManager) watch(port int, path string, session *tunnelclient.Session) {
	<-session.Done()
	if err := session.Err(); err != nil {
		var apiErr *tunnelclient.APIError
		if errors.As(err, &apiErr) && (apiErr.Code == "claim_released" || apiErr.Code == "claim_expired") {
			tm.removeTerminalSession(port, path, session)
			return
		}
		// Bespoke, not setStatus: the write must be conditional on this
		// session still being the path's current one — a release+reclaim
		// may have replaced it while we waited.
		tm.mu.Lock()
		if t := tm.tunnels[port]; t != nil && t.sessions[path] == session {
			t.Status, t.LastError = statusError, err.Error()
		}
		tm.mu.Unlock()
	}
}

func (tm *TunnelManager) removeTerminalSession(port int, path string, session *tunnelclient.Session) {
	tm.mu.Lock()
	tunnel := tm.tunnels[port]
	if tunnel == nil || tunnel.sessions[path] != session {
		tm.mu.Unlock()
		return
	}
	delete(tunnel.sessions, path)
	tunnel.rebuildPaths()
	empty := len(tunnel.sessions) == 0
	if empty {
		delete(tm.tunnels, port)
	}
	tm.mu.Unlock()
	if empty && tunnel.stopProxy != nil {
		tunnel.stopProxy()
	}
}
func (tm *TunnelManager) fail(port int, err error) error {
	tm.setStatus(port, statusError, err.Error())
	return err
}
func (tm *TunnelManager) detach(port int) *Tunnel {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t := tm.tunnels[port]
	delete(tm.tunnels, port)
	return t
}
func closeTunnel(t *Tunnel) {
	if t == nil {
		return
	}
	for _, session := range t.sessions {
		_ = session.Close()
	}
	if t.stopProxy != nil {
		t.stopProxy()
	}
}
func (tm *TunnelManager) releasePort(port int) []string {
	tm.opMu.Lock()
	defer tm.opMu.Unlock()
	tunnel := tm.detach(port)
	if tunnel == nil {
		return nil
	}
	paths := append([]string(nil), tunnel.Paths...)
	closeTunnel(tunnel)
	return paths
}
func (tm *TunnelManager) releasePath(req ReleaseAPIRequest) error {
	tm.opMu.Lock()
	defer tm.opMu.Unlock()
	normalized, err := tunnelclient.NormalizePath(req.Path)
	if err != nil {
		return fmt.Errorf("release %q: %w", req.Path, err)
	}
	path := normalized
	tm.mu.Lock()
	var session *tunnelclient.Session
	var emptied *Tunnel
	for port, t := range tm.tunnels {
		if t.sessions[path] != nil {
			session = t.sessions[path]
			delete(t.sessions, path)
			t.rebuildPaths()
			if len(t.sessions) == 0 {
				delete(tm.tunnels, port)
				emptied = t
			}
			break
		}
	}
	tm.mu.Unlock()
	if session == nil {
		client, err := tm.client(&ClientAPIOptions{
			Gateway: req.Gateway, Token: req.Token, Insecure: req.Insecure,
		})
		if err != nil {
			return err
		}
		claims, err := client.List(context.Background())
		if err != nil {
			return err
		}
		for _, claim := range claims {
			for _, claimedPath := range claim.Paths {
				if claimedPath == path {
					return client.Release(context.Background(), claim.ID)
				}
			}
		}
		return fmt.Errorf("path %q is not claimed", path)
	}
	_ = session.Close()
	// closeTunnel owns the rest of the teardown (the emptied tunnel has
	// no sessions left, so this only stops its proxy).
	closeTunnel(emptied)
	return nil
}
func (tm *TunnelManager) ReleaseAllOnShutdown() {
	tm.mu.RLock()
	var ports []int
	for p := range tm.tunnels {
		ports = append(ports, p)
	}
	tm.mu.RUnlock()
	for _, p := range ports {
		tm.releasePort(p)
	}
}

type ClientAPIOptions struct {
	Gateway  string `json:"gateway,omitempty"`
	Token    string `json:"token,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`
}
type ClaimAPIRequest struct {
	LocalPort int      `json:"local_port"`
	Paths     []string `json:"paths"`
	Gateway   string   `json:"gateway,omitempty"`
	Token     string   `json:"token,omitempty"`
	Insecure  bool     `json:"insecure,omitempty"`
}
type ReleaseAPIRequest struct {
	LocalPort int    `json:"local_port,omitempty"`
	Path      string `json:"path,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
	Token     string `json:"token,omitempty"`
	Insecure  bool   `json:"insecure,omitempty"`
}
type ReleaseAPIResponse struct {
	daemon.APIResponse
	Released  int      `json:"released"`
	Paths     []string `json:"paths"`
	LocalPort int      `json:"local_port,omitempty"`
	Gateway   string   `json:"gateway,omitempty"`
}
type TunnelView struct {
	LocalPort int      `json:"local_port"`
	ProxyPort int      `json:"proxy_port"`
	Paths     []string `json:"paths"`
	Status    string   `json:"status"`
	LastError string   `json:"last_error,omitempty"`
}
type TunnelListResponse struct {
	Gateway string           `json:"gateway,omitempty"`
	Tunnels []TunnelView     `json:"tunnels"`
	Claims  []LocalClaimView `json:"claims,omitempty"`
}
type LocalClaimView struct {
	Paths     []string `json:"paths"`
	Owner     string   `json:"owner"`
	StartedAt string   `json:"started_at"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	LocalPort int      `json:"local_port"`
	Status    string   `json:"status"`
}
type GlobalClaimView struct {
	PathPrefix string `json:"path_prefix"`
	Owner      string `json:"owner"`
	StartedAt  string `json:"started_at"`
	ExpiresAt  string `json:"expires_at"`
	Mine       bool   `json:"mine"`
}
type GlobalClaimsResponse struct {
	Claims []GlobalClaimView `json:"claims"`
}

// TunnelFeature adapts a TunnelManager to the daemon's HTTP mux.
type TunnelFeature struct{ tm *TunnelManager }

// NewTunnelFeature wraps tm for HTTP registration (mux.HandleFunc(..., feat.HandleTunnel)).
func NewTunnelFeature(tm *TunnelManager) *TunnelFeature {
	return &TunnelFeature{tm: tm}
}

// HandleTunnel serves GET/POST /api/tunnel and /api/tunnel/{claim,release}.
func (f *TunnelFeature) HandleTunnel(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Get("all") == "true":
		f.tunnelListAll(w, r, nil)
	case r.Method == http.MethodGet:
		daemon.WriteJSON(w, http.StatusOK, f.tm.Views())
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/list"):
		var options ClientAPIOptions
		if json.NewDecoder(r.Body).Decode(&options) != nil {
			daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "invalid body"})
			return
		}
		f.tunnelListAll(w, r, &options)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/claim"):
		f.tunnelClaim(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/release"):
		f.tunnelRelease(w, r)
	default:
		daemon.WriteJSON(w, http.StatusMethodNotAllowed, daemon.APIResponse{Error: "method not allowed"})
	}
}

// Views returns a wire snapshot of all tunnels, sorted by local port.
func (tm *TunnelManager) Views() TunnelListResponse {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	// Non-nil even when empty: the wire shape is "tunnels":[] — null
	// would crash the dashboard's filter on first load.
	views := make([]TunnelView, 0, len(tm.tunnels))
	claims := make([]LocalClaimView, 0)
	for _, t := range tm.tunnels {
		views = append(views, TunnelView{
			LocalPort: t.LocalPort,
			ProxyPort: t.ProxyPort,
			Paths:     append([]string(nil), t.Paths...),
			Status:    t.Status,
			LastError: t.LastError,
		})
		for _, session := range t.sessions {
			claim := session.Claim()
			expiresAt := ""
			if claim.ExpiresAt != nil {
				expiresAt = claim.ExpiresAt.Format(time.RFC3339Nano)
			}
			claims = append(claims, LocalClaimView{
				Paths: append([]string(nil), claim.Paths...), Owner: claim.Owner,
				StartedAt: claim.StartedAt.Format(time.RFC3339Nano), ExpiresAt: expiresAt,
				LocalPort: t.LocalPort, Status: "connected",
			})
		}
	}
	sort.Slice(views, func(i, j int) bool { return views[i].LocalPort < views[j].LocalPort })
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].LocalPort != claims[j].LocalPort {
			return claims[i].LocalPort < claims[j].LocalPort
		}
		return strings.Join(claims[i].Paths, "\x00") < strings.Join(claims[j].Paths, "\x00")
	})
	gateway := ""
	if c := ClaimFrom(tm.host.Config()); c != nil {
		gateway = c.Gateway
	}
	return TunnelListResponse{Gateway: gateway, Tunnels: views, Claims: claims}
}
func (f *TunnelFeature) tunnelListAll(w http.ResponseWriter, r *http.Request, options *ClientAPIOptions) {
	client, err := f.tm.client(options)
	if err != nil {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: err.Error()})
		return
	}
	claims, err := client.List(r.Context())
	if err != nil {
		daemon.WriteJSON(w, http.StatusBadGateway, daemon.APIResponse{Error: fmt.Sprintf("cannot reach Tunlease gateway at %s: %v", client.Gateway(), err)})
		return
	}
	f.tm.mu.RLock()
	mine := map[string]bool{}
	for _, t := range f.tm.tunnels {
		for _, session := range t.sessions {
			mine[session.Claim().ID] = true
		}
	}
	f.tm.mu.RUnlock()
	views := make([]GlobalClaimView, 0, len(claims))
	for _, claim := range claims {
		expiresAt := ""
		if claim.ExpiresAt != nil {
			expiresAt = claim.ExpiresAt.Format(time.RFC3339)
		}
		for _, path := range claim.Paths {
			views = append(views, GlobalClaimView{
				PathPrefix: path,
				Owner:      claim.Owner,
				StartedAt:  claim.StartedAt.Format(time.RFC3339Nano),
				ExpiresAt:  expiresAt,
				Mine:       mine[claim.ID],
			})
		}
	}
	daemon.WriteJSON(w, http.StatusOK, GlobalClaimsResponse{Claims: views})
}
func (f *TunnelFeature) tunnelClaim(w http.ResponseWriter, r *http.Request) {
	var req ClaimAPIRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.LocalPort == 0 || len(req.Paths) == 0 {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "local_port and paths required"})
		return
	}
	if err := f.tm.claim(req); err != nil {
		// 200 with Error is the established tunnel wire contract: the
		// CLI and dashboard branch on the Error field, not the status.
		daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{Error: err.Error()})
		return
	}
	daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, Message: "claim active"})
}
func (f *TunnelFeature) tunnelRelease(w http.ResponseWriter, r *http.Request) {
	var req ReleaseAPIRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		daemon.WriteJSON(w, http.StatusBadRequest, daemon.APIResponse{Error: "invalid body"})
		return
	}
	if req.LocalPort != 0 {
		client, err := f.tm.client(&ClientAPIOptions{
			Gateway: req.Gateway, Token: req.Token, Insecure: req.Insecure,
		})
		if err != nil {
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{Error: err.Error()})
			return
		}
		paths := f.tm.releasePort(req.LocalPort)
		daemon.WriteJSON(w, http.StatusOK, ReleaseAPIResponse{
			APIResponse: daemon.APIResponse{OK: true, Message: "released"},
			Released:    len(paths), Paths: paths, LocalPort: req.LocalPort, Gateway: client.Gateway(),
		})
		return
	} else if req.Path != "" {
		if err := f.tm.releasePath(req); err != nil {
			daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{Error: err.Error()})
			return
		}
		daemon.WriteJSON(w, http.StatusOK, ReleaseAPIResponse{
			APIResponse: daemon.APIResponse{OK: true, Message: "released"},
			Released:    1, Paths: []string{req.Path},
		})
		return
	}
	daemon.WriteJSON(w, http.StatusOK, daemon.APIResponse{OK: true, Message: "released"})
}

// SnapshotTunnels converts a tunnel listing into daemon resource snapshots.
func SnapshotTunnels(list TunnelListResponse) []daemon.ResourceSnapshot {
	var out []daemon.ResourceSnapshot
	for _, t := range list.Tunnels {
		name := fmt.Sprintf("tunnel-%d", t.LocalPort)
		p := map[string]string{"local_port": strconv.Itoa(t.LocalPort), "proxy_port": strconv.Itoa(t.ProxyPort)}
		if list.Gateway != "" {
			p["gateway"] = list.Gateway
		}
		out = append(out, daemon.ResourceSnapshot{Name: name, Type: "tunnel", State: t.Status, StateReason: t.LastError, Properties: p})
		for _, path := range t.Paths {
			out = append(out, daemon.ResourceSnapshot{Name: path, Type: "route", State: t.Status, Parent: name})
		}
	}
	return out
}
