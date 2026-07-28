package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CLIOriginHeader marks requests issued by the orbit CLI so the daemon's
// history middleware can distinguish them from dashboard traffic.
const CLIOriginHeader = "X-Orbit-Origin"

// Client is a thin HTTP client that communicates with the daemon via unix socket.
type Client struct {
	http       *http.Client
	socketPath string
}

// NewClient creates a new daemon client.
func NewClient(socketPath string) *Client {
	return &Client{
		http:       SocketHTTPClient(socketPath),
		socketPath: socketPath,
	}
}

// ErrDaemonUnreachable indicates the daemon socket is unreachable or not
// responding to a health check. Callers can use errors.Is(err, ErrDaemonUnreachable)
// to react specifically. The wrapped message already includes a user-facing
// hint, so most callers can just propagate.
var ErrDaemonUnreachable = errors.New("daemon unreachable")

// Dial creates a client and verifies the daemon is alive. Returns
// ErrDaemonUnreachable wrapped with the underlying cause and a CLI hint
// on failure. Use NewClient instead when you intentionally want a client
// without a health check (e.g. status and doctor commands probe with
// Health() == nil and continue regardless).
func Dial(socketPath string) (*Client, error) {
	c := NewClient(socketPath)
	if err := c.Health(); err != nil {
		return nil, fmt.Errorf("%w — start with 'orbit up' or 'orbit daemon start': %w",
			ErrDaemonUnreachable, err)
	}
	return c, nil
}

// Health checks if the daemon is alive.
func (c *Client) Health() error {
	resp, err := c.http.Get("http://orbit/api/health")
	if err != nil {
		return fmt.Errorf("daemon not reachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned %d", resp.StatusCode)
	}
	return nil
}

// Doctor runs health checks via the daemon.
func (c *Client) Doctor() (*DoctorResponse, error) {
	resp, err := c.http.Get("http://orbit/api/doctor")
	if err != nil {
		return nil, fmt.Errorf("doctor request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result DoctorResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding doctor: %w", err)
	}
	return &result, nil
}

// Status returns all service statuses.
func (c *Client) Status() (*StatusResponse, error) {
	resp, err := c.http.Get("http://orbit/api/status")
	if err != nil {
		return nil, fmt.Errorf("status request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding status: %w", err)
	}
	return &result, nil
}

// TraceLogs returns the log lines that carry the trace id, joined server-side
// where both the trace store and log buffers live.
func (c *Client) TraceLogs(id string) ([]TraceLogLine, error) {
	resp, err := c.http.Get("http://orbit/api/traces/" + id + "/logs")
	if err != nil {
		return nil, fmt.Errorf("trace logs request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, readError(resp)
	}

	var result TraceLogsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding trace logs: %w", err)
	}
	return result.Lines, nil
}

// Envs returns the list of available env configs and which is current.
func (c *Client) Envs() (*EnvsResponse, error) {
	resp, err := c.http.Get("http://orbit/api/envs")
	if err != nil {
		return nil, fmt.Errorf("envs request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result EnvsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding envs: %w", err)
	}
	return &result, nil
}

// Version returns the daemon's build version and upgrade info.
func (c *Client) Version() (*VersionResponse, error) {
	resp, err := c.http.Get("http://orbit/api/version")
	if err != nil {
		return nil, fmt.Errorf("version request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding version: %w", err)
	}
	return &result, nil
}

// Up sends an up request to the daemon.
func (c *Client) Up(req UpRequest) (*APIResponse, error) {
	return c.postJSON("/api/up", req)
}

// Down sends a down request to the daemon.
func (c *Client) Down(all bool) (*APIResponse, error) {
	return c.postJSON("/api/down", DownRequest{All: all})
}

// DownAndWait stops every service and container before returning.
func (c *Client) DownAndWait() (*APIResponse, error) {
	return c.postJSON("/api/down", DownRequest{Wait: true})
}

// Stop stops a single service.
func (c *Client) Stop(name string) (*APIResponse, error) {
	return c.postJSON("/api/stop/"+name, nil)
}

// Restart restarts a single service.
func (c *Client) Restart(name string) (*APIResponse, error) {
	return c.postJSON("/api/restart/"+name, nil)
}

// Logs returns the last N lines for a service.
func (c *Client) Logs(name string, lines int) (*LogsResponse, error) {
	url := fmt.Sprintf("http://orbit/api/logs/%s?lines=%d", name, lines)
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("logs request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, readError(resp)
	}

	var result LogsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding logs: %w", err)
	}
	return &result, nil
}

// StreamLogs opens an SSE connection and calls fn for each line.
// Blocks until the context is cancelled or connection drops.
func (c *Client) StreamLogs(name string, fn func(line string)) error {
	url := fmt.Sprintf("http://orbit/api/logs/%s/stream", name)
	resp, err := c.http.Get(url)
	if err != nil {
		return fmt.Errorf("stream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return readError(resp)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			fn(strings.TrimPrefix(line, "data: "))
		}
	}
	return scanner.Err()
}

// Get issues a raw GET against the daemon socket — the escape hatch for
// extension-owned calls whose error semantics predate GetDecode and must
// stay byte-identical. Prefer GetDecode for new calls.
func (c *Client) Get(path string) (*http.Response, error) {
	return c.http.Get("http://orbit" + path)
}

// ReadAPIError maps a non-200 response through the daemon error
// contract — exported for extension-owned raw reads.
func ReadAPIError(resp *http.Response) error {
	return readError(resp)
}

// FastClone returns a client on the same socket with tight timeouts, for
// best-effort notifications that must never block the caller.
func FastClone(c *Client, timeout time.Duration) *Client {
	return &Client{
		http:       SocketHTTPClientWithTimeout(c.socketPath, timeout, timeout),
		socketPath: c.socketPath,
	}
}

// PostJSON posts body to path and decodes the APIResponse — the exported
// primitive extension-owned client calls build on.
func (c *Client) PostJSON(path string, body any) (*APIResponse, error) {
	return c.postJSON(path, body)
}

// PostDecode posts a JSON body and decodes an extension-owned response.
func (c *Client) PostDecode(path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, "http://orbit"+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("building request to %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(CLIOriginHeader, "cli")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s failed: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return readError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding %s response: %w", path, err)
	}
	return nil
}

// GetDecode GETs path and decodes the JSON payload into out, mapping
// non-200s through the daemon error contract — the exported read
// primitive for extension-owned client calls.
func (c *Client) GetDecode(path string, out any) error {
	resp, err := c.http.Get("http://orbit" + path)
	if err != nil {
		return fmt.Errorf("GET %s failed: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return readError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding %s response: %w", path, err)
	}
	return nil
}

func (c *Client) postJSON(path string, body any) (*APIResponse, error) {
	return c.postJSONWithHeaders(path, body, map[string]string{CLIOriginHeader: "cli"})
}

// apiError preserves the daemon's stable code so callers can classify the
// failure without coupling to user-facing message text.
type apiError struct {
	Code    string
	Message string
}

func (e *apiError) Error() string {
	return e.Message
}

func (e *apiError) ErrorCode() string {
	return e.Code
}

func (c *Client) postJSONWithHeaders(path string, body any, headers map[string]string) (*APIResponse, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request: %w", err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader([]byte("{}"))
	}

	req, err := http.NewRequest(http.MethodPost, "http://orbit"+path, reader)
	if err != nil {
		return nil, fmt.Errorf("building request to %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if result.Error != "" {
		if result.Code != "" {
			return &result, &apiError{Code: result.Code, Message: result.Error}
		}
		return &result, fmt.Errorf("%s", result.Error)
	}
	return &result, nil
}

// nolint:unused // called by subsequent tasks
func (c *Client) putJSON(path string, body any) (*APIResponse, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request: %w", err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader([]byte("{}"))
	}

	req, err := http.NewRequest(http.MethodPut, "http://orbit"+path, reader)
	if err != nil {
		return nil, fmt.Errorf("building request to %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(CLIOriginHeader, "cli")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if result.Error != "" {
		return &result, fmt.Errorf("%s", result.Error)
	}
	return &result, nil
}

// SetEdgeDetached marks the edge from→to as detached (true) or attached (false)
// in the daemon's current env.
func (c *Client) SetEdgeDetached(from, to string, detached bool) error {
	path := "/api/edges/" + from + "/" + to
	body := EdgeDetachRequest{Detached: detached}
	if _, err := c.putJSON(path, body); err != nil {
		return fmt.Errorf("set edge %s→%s detached=%v: %w", from, to, detached, err)
	}
	return nil
}

// SetServiceMode switches a dual-defined service between "dev" and "container".
func (c *Client) SetServiceMode(name, mode string) error {
	path := "/api/service-mode/" + name
	if _, err := c.putJSON(path, ServiceModeRequest{Mode: mode}); err != nil {
		return fmt.Errorf("set service mode %s=%s: %w", name, mode, err)
	}
	return nil
}

// SetEnvToggle flips a pre-declared env-var toggle for a service.
func (c *Client) SetEnvToggle(service, varName string, enabled bool) error {
	body := EnvToggleUpdateRequest{Service: service, Var: varName, Enabled: enabled}
	if _, err := c.putJSON("/api/env-toggles", body); err != nil {
		return fmt.Errorf("set env toggle %s/%s=%v: %w", service, varName, enabled, err)
	}
	return nil
}

// GetSettings returns the daemon's current user settings as a flat JSON object.
func (c *Client) GetSettings() (map[string]any, error) {
	resp, err := c.http.Get("http://orbit/api/settings")
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, readError(resp)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding settings: %w", err)
	}
	return out, nil
}

// UpdateSettings applies a partial settings patch. Only keys present in the map
// are sent; the daemon ignores unknown keys.
func (c *Client) UpdateSettings(patch map[string]any) error {
	if _, err := c.putJSON("/api/settings", patch); err != nil {
		return fmt.Errorf("update settings: %w", err)
	}
	return nil
}

func readError(resp *http.Response) error {
	var result APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("HTTP %d: decode response: %w", resp.StatusCode, err)
	}
	if result.Error != "" {
		if result.Code != "" {
			return &apiError{Code: result.Code, Message: result.Error}
		}
		return errors.New(result.Error)
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}
