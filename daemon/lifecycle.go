package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/iml885203/orbit/atomicio"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/instance"
	"github.com/iml885203/orbit/platform"
	"github.com/iml885203/orbit/process"
)

// Sentinel errors classifying daemon start failures so callers can render
// user-facing hints (see cmd/orbit/daemon_start_errors.go) without string
// matching.
var (
	// ErrInvalidConfig means the config file failed to load/validate
	// before the daemon was forked. The wrapped error has the parser
	// diagnostic.
	ErrInvalidConfig = errors.New("invalid config")
	// ErrDaemonExitedEarly means the daemon process died before
	// reaching ready. The wrapped error includes the previously-running
	// PID and a tail of ~/.orbit/daemon.log.
	ErrDaemonExitedEarly = errors.New("daemon exited early")
	// ErrDaemonNotReady means the daemon process is still alive but
	// did not pass health checks within the timeout. The wrapped error
	// includes the PID and a tail of ~/.orbit/daemon.log.
	ErrDaemonNotReady    = errors.New("daemon not ready")
	ErrSocketPathTooLong = errors.New("socket path too long")
)

// ConfigMismatchError prevents a CLI command from silently combining one
// selected environment with a daemon running another.
type ConfigMismatchError struct {
	Requested string
	Running   string
}

// ConfigStaleError prevents commands from acting on a resource graph that no
// longer matches the selected environment file.
type ConfigStaleError struct {
	Reason string
}

// UpdateRequiredError prevents resource commands from crossing CLI/daemon
// versions after a newer Orbit binary has been installed.
type UpdateRequiredError struct {
	Running            string
	Installed          string
	RestartCommand     string
	RestartJSONCommand string
}

func (e *UpdateRequiredError) Error() string {
	command := e.RestartCommand
	if command == "" {
		command = "orbit daemon restart"
	}
	return fmt.Sprintf("an Orbit update is ready — run %s before continuing", command)
}

func (e *ConfigStaleError) Error() string {
	return "environment changes are pending — run 'orbit env apply' before continuing"
}

func (e *ConfigMismatchError) Error() string {
	if strings.TrimSpace(e.Running) == "" {
		return fmt.Sprintf(
			"the running daemon does not report its active config (it may be an older Orbit build) — run 'orbit daemon restart -c %q' before continuing",
			e.Requested,
		)
	}
	return fmt.Sprintf(
		"selected config %q differs from the running daemon config %q — run 'orbit daemon restart -c %q' to use the selected config, or rerun with '-c %q' to address the running environment",
		e.Requested, e.Running, e.Requested, e.Running,
	)
}

// CheckConfigMatch fails closed when an older daemon cannot identify its
// config. Continuing would let a newly selected environment drive resources
// or destructive extension commands in the daemon's unknown environment.
func CheckConfigMatch(requested, running string) error {
	if strings.TrimSpace(requested) == "" {
		return nil
	}
	if strings.TrimSpace(running) == "" {
		return &ConfigMismatchError{Requested: normalizedConfigPath(requested)}
	}
	requested = normalizedConfigPath(requested)
	running = normalizedConfigPath(running)
	if requested == running {
		return nil
	}
	return &ConfigMismatchError{Requested: requested, Running: running}
}

func CheckEnvironmentReconciled(status *StatusResponse) error {
	if status == nil || !status.ConfigStale {
		return nil
	}
	return &ConfigStaleError{Reason: status.ConfigStaleReason}
}

func CheckDaemonCurrent(version *VersionResponse) error {
	if version == nil || !version.UpdateAvailable {
		return nil
	}
	return &UpdateRequiredError{
		Running:   version.Running,
		Installed: version.OnDisk,
	}
}

func normalizedConfigPath(path string) string {
	path = strings.TrimSpace(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

// DefaultPIDPath returns ~/.orbit/orbit.pid.
func DefaultPIDPath() string {
	return filepath.Join(OrbitDir(), "orbit.pid")
}

// DefaultLogPath returns ~/.orbit/daemon.log.
func DefaultLogPath() string {
	return filepath.Join(OrbitDir(), "daemon.log")
}

// WritePID writes the current process PID to the PID file.
func WritePID() error {
	path := DefaultPIDPath()
	data, err := json.Marshal(pidRecord{
		PID:           os.Getpid(),
		DashboardPort: DashboardPort(),
	})
	if err != nil {
		return fmt.Errorf("encoding daemon ownership: %w", err)
	}
	return atomicio.WriteFile(path, append(data, '\n'), 0644)
}

// ReadPID reads the PID from the PID file. Returns 0 if not found.
func ReadPID() int {
	return readPIDRecord().PID
}

type pidRecord struct {
	PID           int `json:"pid"`
	DashboardPort int `json:"dashboard_port"`
}

func readPIDRecord() pidRecord {
	data, err := os.ReadFile(DefaultPIDPath())
	if err != nil {
		return pidRecord{}
	}
	var record pidRecord
	if json.Unmarshal(data, &record) == nil && record.PID > 0 {
		return record
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return pidRecord{}
	}
	return pidRecord{PID: pid, DashboardPort: configuredDashboardPortOrDefault()}
}

// RemovePID removes the PID file.
func RemovePID() {
	_ = os.Remove(DefaultPIDPath())
}

// IsProcessAlive checks if a process with the given PID exists.
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return platform.IsProcessAlive(pid)
}

// IsDaemonRunning checks if a daemon is already running by checking the PID file
// and verifying the process is alive.
func IsDaemonRunning() (int, bool) {
	pid := ReadPID()
	if pid == 0 {
		return 0, false
	}
	if !IsProcessAlive(pid) {
		// Stale PID file — clean up
		RemovePID()
		_ = os.Remove(DefaultSocketPath())
		return 0, false
	}
	return pid, true
}

// EnsureDaemon checks if a daemon is running. If not, starts one.
// Returns a Client connected to the daemon.
//
// On failure the error is wrapped with one of the sentinels above so the
// CLI can render an actionable hint. Failures that surface a daemon-side
// problem (early exit, not ready) also carry the last lines written to
// ~/.orbit/daemon.log since the fork, so the user does not have to
// `cat` the log to see why.
func EnsureDaemon(configPath string, features []string) (*Client, error) {
	return EnsureDaemonWithContext(configPath, features, "")
}

func EnsureDaemonWithContext(configPath string, features []string, contextKind string) (*Client, error) {
	return EnsureDaemonWithOperationContext(context.Background(), configPath, features, contextKind)
}

func EnsureDaemonWithOperationContext(ctx context.Context, configPath string, features []string, contextKind string) (*Client, error) {
	if pid, alive := IsDaemonRunning(); alive {
		// Verify we can actually connect
		client := NewClient(DefaultSocketPath()).WithContext(ctx)
		if err := client.Health(); err == nil {
			if status, statusErr := client.Status(); statusErr == nil {
				if mismatch := CheckConfigMatch(configPath, status.ConfigPath); mismatch != nil {
					return nil, mismatch
				}
			}
			return client, nil
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// A stale PID can be reused after reboot. The dashboard listener is
		// independent ownership evidence; without it, killing the recorded PID
		// could terminate an unrelated user process.
		record := readPIDRecord()
		if record.PID == pid && daemonOwnsDashboardPort(pid, record.DashboardPort) {
			if err := retireUnreachableDaemon(ctx, pid); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrDaemonNotReady, err)
			}
		}
		RemovePID()
		_ = os.Remove(DefaultSocketPath())
	}

	if err := ValidateSocketPath(DefaultSocketPath()); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSocketPathTooLong, err)
	}

	// Pre-validate config before forking. A schema/parse error here would
	// otherwise kill the daemon child in its first 100ms and leave the
	// CLI waiting through the readiness timeout with no on-screen reason.
	if _, err := config.Load(configPath); err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrInvalidConfig, configPath, err)
	}

	// Capture the daemon log offset before fork so we can tail only the
	// lines written by *this* attempt on failure.
	logOffset := daemonLogSize()

	pid, err := StartDaemonWithContext(configPath, features, contextKind)
	if err != nil {
		return nil, fmt.Errorf("starting daemon: %w", err)
	}

	client := NewClient(DefaultSocketPath()).WithContext(ctx)
	if err := waitForReadyOrDeath(ctx, client, pid, 30*time.Second); err != nil {
		tail := tailDaemonLog(logOffset, 20)
		switch {
		case errors.Is(err, ErrDaemonExitedEarly):
			return nil, fmt.Errorf("%w (pid %d)%s", ErrDaemonExitedEarly, pid, formatLogTail(tail))
		default:
			return nil, fmt.Errorf("%w within 30s (pid %d still alive)%s", ErrDaemonNotReady, pid, formatLogTail(tail))
		}
	}

	return client, nil
}

func daemonOwnsDashboardPort(pid, port int) bool {
	if port <= 0 {
		return false
	}
	for _, holder := range process.FindPortHolders([]int{port}) {
		if holder.PID == pid {
			return true
		}
	}
	return false
}

func retireUnreachableDaemon(ctx context.Context, pid int) error {
	_ = platform.SendTermSignal(pid)
	if exited, err := waitForProcessExit(ctx, pid, 3*time.Second); err != nil {
		return err
	} else if exited {
		return nil
	}
	_ = platform.SendKillSignal(pid)
	if exited, err := waitForProcessExit(ctx, pid, 2*time.Second); err != nil {
		return err
	} else if exited {
		return nil
	}
	return fmt.Errorf("owned Orbit daemon pid %d did not exit after termination", pid)
}

func waitForProcessExit(ctx context.Context, pid int, timeout time.Duration) (bool, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !platform.IsProcessAlive(pid) {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			return !platform.IsProcessAlive(pid), nil
		case <-ticker.C:
		}
	}
}

// StartDaemon forks a new daemon process in a new session. Returns the
// child PID so callers can poll for early exit while waiting for ready.
func StartDaemon(configPath string, features []string) (int, error) {
	return StartDaemonWithContext(configPath, features, "")
}

func StartDaemonWithContext(configPath string, features []string, contextKind string) (int, error) {
	// Respect explicitly pinned ports, while allowing the default setup to
	// coexist with another Orbit instance without user configuration. The
	// child validates the selected port again after the close/fork gap.
	preferredDashboardPort, pinnedDashboardPort := dashboardPortFromEnv()
	if instance.CurrentName() != "" {
		var err error
		preferredDashboardPort, err = instance.ResolveDashboardPort(preferredDashboardPort)
		if err != nil {
			return 0, fmt.Errorf("resolving instance dashboard port: %w", err)
		}
		pinnedDashboardPort = true
	}
	dashboardPort, err := selectDashboardPort(preferredDashboardPort, pinnedDashboardPort)
	if err != nil {
		return 0, err
	}

	configPath = strings.TrimSpace(configPath)

	// Resolve to absolute path so the daemon (with different cwd) can find it
	if !filepath.IsAbs(configPath) {
		if abs, err := filepath.Abs(configPath); err == nil {
			configPath = abs
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("finding executable: %w", err)
	}

	args := []string{"daemon", "run", "--config", configPath}
	if contextKind != "" {
		args = append(args, "--context-kind", contextKind)
	}
	for _, f := range features {
		args = append(args, "--feature", f)
	}

	logFile, err := OpenDaemonLog()
	if err != nil {
		return 0, err
	}

	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "ORBIT_DASHBOARD_PORT="+strconv.Itoa(dashboardPort))
	if configPath != "" {
		cmd.Dir = filepath.Dir(configPath)
	}
	platform.DetachProcess(cmd)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// Detach stdin so daemon doesn't hold terminal
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return 0, fmt.Errorf("starting daemon process: %w", err)
	}

	pid := cmd.Process.Pid
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()

	return pid, nil
}

// waitForReadyOrDeath polls health and process liveness in lockstep so a
// daemon that dies in its first second surfaces a sub-second
// ErrDaemonExitedEarly instead of a 30s timeout. Returns nil on ready,
// ErrDaemonExitedEarly if the PID disappears, or a plain timeout error
// (which EnsureDaemon converts to ErrDaemonNotReady) on deadline.
func waitForReadyOrDeath(ctx context.Context, client *Client, pid int, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("daemon did not become ready within %s", timeout)
		case <-ticker.C:
			if err := client.Health(); err == nil {
				return nil
			}
			if !platform.IsProcessAlive(pid) {
				return ErrDaemonExitedEarly
			}
		}
	}
}

// daemonLogSize returns the current size of ~/.orbit/daemon.log in
// bytes, or 0 if the file doesn't exist yet. Used as a marker so
// tailDaemonLog can return only lines written after this point.
func daemonLogSize() int64 {
	info, err := os.Stat(DefaultLogPath())
	if err != nil {
		return 0
	}
	return info.Size()
}

// tailDaemonLog reads daemon.log starting at offset and returns up to
// maxLines of the most recent lines. Returns "" if the file is missing,
// empty since offset, or unreadable — callers should not depend on
// content being present.
func tailDaemonLog(offset int64, maxLines int) string {
	f, err := os.Open(DefaultLogPath())
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(offset, 0); err != nil {
		return ""
	}
	// Ring buffer of the last maxLines lines. Daemon failures usually
	// write < 50 lines before exiting, so a full read is acceptable.
	lines := make([]string, 0, maxLines)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if len(lines) == maxLines {
			lines = lines[1:]
		}
		lines = append(lines, sc.Text())
	}
	return strings.Join(lines, "\n")
}

// formatLogTail prepends a header to the tail content so error messages
// have a consistent shape. Returns "" if tail is empty so the caller's
// format string doesn't leave a dangling header.
func formatLogTail(tail string) string {
	if tail == "" {
		return ""
	}
	return "\n\nLast lines from " + DefaultLogPath() + ":\n" + indentLines(tail, "  ")
}

func indentLines(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// Cleanup removes socket and PID file. Called on daemon exit.
func Cleanup() {
	_ = os.Remove(DefaultSocketPath())
	RemovePID()
}

// OpenDaemonLog opens the daemon log for appending, creating the home
// directory if it does not exist yet. Opening a file does not create its
// parent, and every caller here reached for the same two steps.
func OpenDaemonLog() (*os.File, error) {
	if _, err := EnsureOrbitDir(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(DefaultLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening daemon log: %w", err)
	}
	return f, nil
}

// SocketPath returns the socket path, checking ORBIT_SOCKET env var first.
func SocketPath() string {
	if s := os.Getenv("ORBIT_SOCKET"); s != "" {
		return s
	}
	return DefaultSocketPath()
}

// defaultDashboardPort is the TCP port the dashboard listens on by default.
const defaultDashboardPort = 19800

// DashboardPort returns the active dashboard port. An explicit
// ORBIT_DASHBOARD_PORT remains pinned; otherwise a running daemon's selected
// fallback is read from its ownership record.
func DashboardPort() int {
	if port, pinned := dashboardPortFromEnv(); pinned {
		return port
	}
	record := readPIDRecord()
	if record.PID > 0 && record.DashboardPort > 0 && IsProcessAlive(record.PID) {
		return record.DashboardPort
	}
	return defaultDashboardPort
}

func dashboardPortFromEnv() (int, bool) {
	if s := os.Getenv("ORBIT_DASHBOARD_PORT"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n, true
		}
	}
	return defaultDashboardPort, false
}

func configuredDashboardPortOrDefault() int {
	if port, pinned := dashboardPortFromEnv(); pinned {
		return port
	}
	return defaultDashboardPort
}
