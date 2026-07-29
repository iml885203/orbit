package health

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/logging"
)

// ContainerInspector is the narrow Docker-facing surface the exec and
// healthcheck wait strategies rely on. container.Manager implements it;
// tests inject fakes.
type ContainerInspector interface {
	// ExecInContainer runs cmd inside the managed container for service,
	// returning the process exit code.
	ExecInContainer(ctx context.Context, service string, cmd []string) (int, error)
	// HealthStatus returns the Docker-reported Health.Status
	// ("starting", "healthy", "unhealthy") for the managed container.
	HealthStatus(ctx context.Context, service string) (string, error)
}

type Checker struct {
	httpClient *http.Client
	mux        *logging.Multiplexer // nil disables the "log" strategy
	inspector  ContainerInspector   // nil disables the "exec" / "healthcheck" strategies
	progress   *progressTracker
	// recoveryInterval is the cadence for post-budget recovery probing —
	// gentler than startup probing because the common case is a service
	// that needs more warm-up than the retry budget allowed, not one
	// flapping per-second. Overridden only in tests.
	recoveryInterval time.Duration
}

// NewChecker builds a Checker. Pass nil for dependencies you don't need:
// log strategy wants mux, exec/healthcheck want inspector.
func NewChecker(mux *logging.Multiplexer, inspector ContainerInspector) *Checker {
	return &Checker{
		httpClient:       &http.Client{Timeout: 5 * time.Second},
		mux:              mux,
		inspector:        inspector,
		progress:         newProgressTracker(),
		recoveryInterval: 10 * time.Second,
	}
}

// Check performs a single health check based on config.
func (c *Checker) Check(ctx context.Context, name string, hc *config.HealthCheckConfig) Result {
	if hc == nil {
		return Result{Service: name, Healthy: true, Message: "no health check configured"}
	}

	start := time.Now()

	switch hc.Type {
	case "http":
		return c.checkHTTP(ctx, name, hc, start)
	case "tcp":
		return c.checkTCP(ctx, name, hc, start)
	case "exec":
		return c.checkExec(ctx, name, hc, start)
	case "healthcheck":
		return c.checkContainerHealth(ctx, name, start)
	default:
		return Result{Service: name, Healthy: false, Message: fmt.Sprintf("unknown check type: %s", hc.Type)}
	}
}

func (c *Checker) checkHTTP(ctx context.Context, name string, hc *config.HealthCheckConfig, start time.Time) Result {
	url := fmt.Sprintf("http://localhost:%d%s", hc.Port, hc.Path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return Result{Service: name, Healthy: false, Message: err.Error(), Latency: time.Since(start)}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{Service: name, Healthy: false, Message: err.Error(), Latency: time.Since(start)}
	}
	_ = resp.Body.Close()

	healthy := resp.StatusCode >= 200 && resp.StatusCode < 300
	return Result{
		Service: name,
		Healthy: healthy,
		Message: fmt.Sprintf("HTTP %d", resp.StatusCode),
		Latency: time.Since(start),
	}
}

func (c *Checker) checkTCP(ctx context.Context, name string, hc *config.HealthCheckConfig, start time.Time) Result {
	timeout := hc.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	addr := fmt.Sprintf("localhost:%d", hc.Port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return Result{Service: name, Healthy: false, Message: err.Error(), Latency: time.Since(start)}
	}
	_ = conn.Close()

	return Result{
		Service: name,
		Healthy: true,
		Message: "TCP connection OK",
		Latency: time.Since(start),
	}
}

func (c *Checker) checkExec(ctx context.Context, name string, hc *config.HealthCheckConfig, start time.Time) Result {
	if c.inspector == nil {
		return Result{Service: name, Healthy: false, Message: "exec strategy requires a container inspector"}
	}
	if len(hc.Command) == 0 {
		return Result{Service: name, Healthy: false, Message: "exec strategy requires command"}
	}
	code, err := c.inspector.ExecInContainer(ctx, name, hc.Command)
	message := fmt.Sprintf("exec exit=%d", code)
	if err != nil {
		message = fmt.Sprintf("exec error: %v", err)
	}
	return Result{
		Service: name,
		Healthy: err == nil && code == 0,
		Message: message,
		Latency: time.Since(start),
	}
}

func (c *Checker) checkContainerHealth(ctx context.Context, name string, start time.Time) Result {
	if c.inspector == nil {
		return Result{Service: name, Healthy: false, Message: "healthcheck strategy requires a container inspector"}
	}
	status, err := c.inspector.HealthStatus(ctx, name)
	message := fmt.Sprintf("health=%s", status)
	if err != nil {
		message = fmt.Sprintf("inspect error: %v", err)
	}
	return Result{
		Service: name,
		Healthy: err == nil && status == "healthy",
		Message: message,
		Latency: time.Since(start),
	}
}

// WaitForHealthy polls health checks until the service is healthy or context is cancelled.
func (c *Checker) WaitForHealthy(ctx context.Context, name string, hc *config.HealthCheckConfig, onResult func(Result)) error {
	if hc == nil {
		r := Result{Service: name, Healthy: true, Message: "no health check configured"}
		if onResult != nil {
			onResult(r)
		}
		return nil
	}

	switch hc.Type {
	case "log":
		return c.waitForLog(ctx, name, hc, onResult)
	case "exec":
		return c.waitForExec(ctx, name, hc, onResult)
	case "healthcheck":
		return c.waitForHealthcheck(ctx, name, hc, onResult)
	case "http", "tcp":
		return c.pollWithProbe(ctx, name, hc, onResult, func(ctx context.Context) Result {
			return c.Check(ctx, name, hc)
		})
	default:
		return fmt.Errorf("health check for %s: unknown type %q", name, hc.Type)
	}
}

// pollWithProbe is the shared retry skeleton used by every interval-based
// wait strategy. probe is called immediately and then every hc.Interval until
// it returns Healthy, the retry budget is exhausted, or ctx is cancelled.
func (c *Checker) pollWithProbe(ctx context.Context, name string, hc *config.HealthCheckConfig, onResult func(Result), probe func(context.Context) Result) error {
	interval, retries := intervalRetries(hc)

	c.resetProgress(name)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Probe once immediately — don't make a service that's already ready
	// wait for the first tick.
	first := true
	for i := 0; i < retries; i++ {
		if !first {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
		first = false

		result := probe(ctx)
		if onResult != nil {
			onResult(result)
		}
		if result.Healthy {
			c.recordProgress(name, true, i+1, retries, nil)
			return nil
		}
		c.recordProgress(name, true, i+1, retries, errResult(result))
	}
	return fmt.Errorf("health check for %s failed after %d retries", name, retries)
}

// ErrRecoveryUnsupported reports that a health-check strategy has no
// single-probe entry point to recover with. Callers gate on
// SupportsRecovery instead of interpreting a nil error as "recovered".
var ErrRecoveryUnsupported = errors.New("health check strategy does not support recovery probing")

// SupportsRecovery reports whether RecoverHealthy can probe this check.
// Log checks are readiness signals: after a pattern appears there is no
// meaningful inverse probe. Every other strategy has a reusable point probe.
func SupportsRecovery(hc *config.HealthCheckConfig) bool {
	if hc == nil {
		return false
	}
	switch hc.Type {
	case "http", "tcp", "exec", "healthcheck":
		return true
	default:
		return false
	}
}

// RecoverHealthy keeps probing a service after the startup retry budget is
// spent, until the probe succeeds or ctx is cancelled. It exists so a
// degraded verdict is not terminal: a service whose first requests were slow
// (dependency warm-up, JIT, cold caches) flips back to healthy without a
// manual restart. Progress is flagged Recovering with a live LastErr while
// the loop runs, so status/UI can show that probing continues.
func (c *Checker) RecoverHealthy(ctx context.Context, name string, generation int, hc *config.HealthCheckConfig, onResult func(Result)) error {
	if !SupportsRecovery(hc) {
		return ErrRecoveryUnsupported
	}
	_, retries := intervalRetries(hc)

	// Idempotent with the caller's MarkRecovering — kept here too so the
	// flag survives callers that skip the pre-announcement.
	c.MarkRecovering(name, generation)

	ticker := time.NewTicker(c.recoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.clearRecovering(name, generation)
			return ctx.Err()
		case <-ticker.C:
		}
		result := c.Check(ctx, name, hc)
		// A probe in flight across a stop/restart can report success after
		// cancellation (checkTCP dials without honoring ctx) — re-check so
		// a cancelled loop never reports a recovery it no longer owns.
		if ctx.Err() != nil {
			c.clearRecovering(name, generation)
			return ctx.Err()
		}
		if result.Healthy {
			c.recordProgress(name, true, retries, retries, nil)
			if onResult != nil {
				onResult(result)
			}
			return nil
		}
		c.recordRecovering(name, generation, errResult(result))
	}
}

// MonitorHealthy continuously verifies a resource after startup. A small
// consecutive-failure budget prevents transient network or scheduler noise
// from flapping the environment. Once degraded, one successful probe is
// enough to recover because the probe itself is the recovery evidence.
func (c *Checker) MonitorHealthy(ctx context.Context, name string, hc *config.HealthCheckConfig, onResult func(Result)) error {
	if !SupportsRecovery(hc) {
		return ErrRecoveryUnsupported
	}
	interval, _ := intervalRetries(hc)
	threshold := hc.FailureThreshold
	if threshold == 0 {
		threshold = config.DefaultHealthFailureThreshold
	}
	failures := 0
	degraded := false
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		result := c.Check(ctx, name, hc)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if result.Healthy {
			failures = 0
			if degraded {
				degraded = false
				if onResult != nil {
					onResult(result)
				}
			}
			continue
		}
		failures++
		if failures < threshold {
			continue
		}
		degraded = true
		if onResult != nil {
			onResult(result)
		}
	}
}

// errResult turns a failed probe result into an error for progress recording.
// Result.Message is the human-facing diagnostic that ends up in LastErr.
func errResult(r Result) error {
	if r.Message == "" {
		return fmt.Errorf("health check failed")
	}
	return fmt.Errorf("%s", r.Message)
}

// waitForLog watches a service's log stream for a regex match, returning once
// a line matches or with a timeout/context error. It is event-driven (not
// polled) — healthy state is declared the moment the first matching line
// arrives, eliminating the poll-interval overshoot of http/tcp checks.
func (c *Checker) waitForLog(ctx context.Context, name string, hc *config.HealthCheckConfig, onResult func(Result)) error {
	if c.mux == nil {
		return fmt.Errorf("health check for %s: log strategy requires a multiplexer (nil mux)", name)
	}
	re, err := regexp.Compile(hc.Pattern)
	if err != nil {
		return fmt.Errorf("health check for %s: invalid pattern %q: %w", name, hc.Pattern, err)
	}

	timeout := hc.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	matched := make(chan struct{}, 1)
	unsub := c.mux.Subscribe(func(svc, line string) {
		if svc != name {
			return
		}
		if re.MatchString(line) {
			select {
			case matched <- struct{}{}:
			default:
			}
		}
	})
	defer unsub()

	start := time.Now()
	select {
	case <-matched:
		if onResult != nil {
			onResult(Result{
				Service: name,
				Healthy: true,
				Message: fmt.Sprintf("log pattern %q matched", hc.Pattern),
				Latency: time.Since(start),
			})
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("health check for %s: log pattern %q not seen in %s", name, hc.Pattern, timeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitForExec runs a command inside the container — exit 0 means ready.
// Unlike tcp/http it verifies real functionality (auth, query), so it
// tracks "system ready for work" rather than "listener up".
func (c *Checker) waitForExec(ctx context.Context, name string, hc *config.HealthCheckConfig, onResult func(Result)) error {
	if c.inspector == nil {
		return fmt.Errorf("health check for %s: exec strategy requires a container inspector", name)
	}
	if len(hc.Command) == 0 {
		return fmt.Errorf("health check for %s: exec strategy requires command", name)
	}
	return c.pollWithProbe(ctx, name, hc, onResult, func(ctx context.Context) Result {
		return c.Check(ctx, name, hc)
	})
}

// waitForHealthcheck reads Docker's State.Health.Status — trusts the image's
// own HEALTHCHECK directive. "starting" and "unhealthy" both keep retrying
// because unhealthy can be transient during startup.
func (c *Checker) waitForHealthcheck(ctx context.Context, name string, hc *config.HealthCheckConfig, onResult func(Result)) error {
	if c.inspector == nil {
		return fmt.Errorf("health check for %s: healthcheck strategy requires a container inspector", name)
	}
	return c.pollWithProbe(ctx, name, hc, onResult, func(ctx context.Context) Result {
		return c.Check(ctx, name, hc)
	})
}

// intervalRetries is a safety net for callers that bypass config.Load —
// loaded configs always carry non-zero values via applyDefaults, and the
// fallback shares config's constant so the two paths can't drift apart
// again (an old hardcoded 60 here vs 3 there hid the real budget).
func intervalRetries(hc *config.HealthCheckConfig) (time.Duration, int) {
	interval := hc.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	retries := hc.Retries
	if retries == 0 {
		retries = config.DefaultHealthRetries
	}
	return interval, retries
}
