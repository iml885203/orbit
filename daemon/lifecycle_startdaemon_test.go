package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// StartDaemon must refuse to fork when the dashboard port is already
// held. Otherwise a stale daemon keeps the TCP port and the new one
// silently starts without a dashboard (the exact behaviour that masked
// the "two daemons competing" bug on the user's machine).

func TestStartDaemon_FailsWhenDashboardPortHeld(t *testing.T) {
	holder, port := occupyPort(t)
	defer func() { _ = holder.Close() }()

	t.Setenv("ORBIT_DASHBOARD_PORT", strconv.Itoa(port))

	_, err := StartDaemon("/tmp/does-not-matter.yaml", nil)
	if err == nil {
		t.Fatal("expected StartDaemon to fail when dashboard port is held, got nil")
	}

	var conflict *PortConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected wrapped PortConflictError, got %T: %v", err, err)
	}
	if conflict.Port != port {
		t.Errorf("conflict.Port = %d, want %d", conflict.Port, port)
	}
}

// EnsureDaemon must classify an overlong socket path before forking. The child
// binds the socket and exits, so without this the parent burned the full 30s
// readiness timeout and then blamed a "stuck" daemon no restart could fix.
func TestEnsureDaemon_RejectsOverlongSocketPathBeforeForking(t *testing.T) {
	home := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORBIT_HOME", home)

	start := time.Now()
	_, err := EnsureDaemon(filepath.Join(home, "config.yaml"), nil)
	if !errors.Is(err, ErrSocketPathTooLong) {
		t.Fatalf("error = %T %v, want ErrSocketPathTooLong", err, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %s — the readiness timeout ran instead of failing fast", elapsed)
	}
	if !strings.Contains(err.Error(), "ORBIT_HOME") {
		t.Errorf("error is not actionable: %q", err)
	}
}

func TestCheckConfigMatch(t *testing.T) {
	dir := t.TempDir()
	selected := filepath.Join(dir, "selected.yaml")
	if err := CheckConfigMatch(selected, selected); err != nil {
		t.Fatalf("same config rejected: %v", err)
	}

	err := CheckConfigMatch(selected, filepath.Join(dir, "running.yaml"))
	var mismatch *ConfigMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %T %v, want ConfigMismatchError", err, err)
	}
	if mismatch.Requested != selected {
		t.Errorf("requested = %q, want %q", mismatch.Requested, selected)
	}
}

func TestCheckConfigMatchRejectsOlderDaemonWithoutPath(t *testing.T) {
	selected := "/tmp/selected.yaml"
	err := CheckConfigMatch(selected, "")
	var mismatch *ConfigMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %T %v, want ConfigMismatchError", err, err)
	}
	if mismatch.Requested != normalizedConfigPath(selected) || mismatch.Running != "" {
		t.Fatalf("mismatch = %+v, want requested config and unknown running config", mismatch)
	}
	if got := err.Error(); !strings.Contains(got, "older Orbit build") ||
		!strings.Contains(got, "orbit daemon restart -c") {
		t.Fatalf("error is not actionable: %q", got)
	}
}

func TestCheckEnvironmentReconciledRejectsPendingChanges(t *testing.T) {
	err := CheckEnvironmentReconciled(&StatusResponse{
		ConfigStale:       true,
		ConfigStaleReason: "env file edited",
	})
	var stale *ConfigStaleError
	if !errors.As(err, &stale) {
		t.Fatalf("error = %T %v, want ConfigStaleError", err, err)
	}
	if stale.Reason != "env file edited" {
		t.Fatalf("reason = %q", stale.Reason)
	}
	if err := CheckEnvironmentReconciled(&StatusResponse{}); err != nil {
		t.Fatalf("fresh environment rejected: %v", err)
	}
}

func TestCheckDaemonCurrentRejectsInstalledUpdate(t *testing.T) {
	err := CheckDaemonCurrent(&VersionResponse{
		Running:         "v0.0.1",
		OnDisk:          "v0.0.2",
		UpdateAvailable: true,
	})
	var update *UpdateRequiredError
	if !errors.As(err, &update) {
		t.Fatalf("error = %T %v, want UpdateRequiredError", err, err)
	}
	if update.Running != "v0.0.1" || update.Installed != "v0.0.2" {
		t.Fatalf("update = %+v", update)
	}
	if err := CheckDaemonCurrent(&VersionResponse{}); err != nil {
		t.Fatalf("current daemon rejected: %v", err)
	}
}
