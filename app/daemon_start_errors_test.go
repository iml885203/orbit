package app

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/iml885203/orbit/daemon"
)

func TestWriteDaemonStartError_InvalidConfigShowsEnvHint(t *testing.T) {
	err := fmt.Errorf("%w envs/foo.yaml: unknown field", daemon.ErrInvalidConfig)
	var buf bytes.Buffer
	writeDaemonStartError(&buf, err)
	out := buf.String()
	if !strings.Contains(out, "orbit env list") {
		t.Errorf("expected env hint, got:\n%s", out)
	}
	if !strings.Contains(out, "invalid config") {
		t.Errorf("expected error message in output, got:\n%s", out)
	}
}

func TestWriteDaemonStartError_NotReadyShowsRestartHint(t *testing.T) {
	err := fmt.Errorf("%w within 30s (pid 123 still alive)", daemon.ErrDaemonNotReady)
	var buf bytes.Buffer
	writeDaemonStartError(&buf, err)
	out := buf.String()
	if !strings.Contains(out, "orbit daemon stop") {
		t.Errorf("expected stop hint, got:\n%s", out)
	}
	if !strings.Contains(out, "orbit daemon restart") {
		t.Errorf("expected restart hint, got:\n%s", out)
	}
}

func TestWriteDaemonStartError_ExitedEarlyShowsLogHint(t *testing.T) {
	err := fmt.Errorf("%w (pid 123)", daemon.ErrDaemonExitedEarly)
	var buf bytes.Buffer
	writeDaemonStartError(&buf, err)
	out := buf.String()
	if !strings.Contains(out, daemon.DefaultLogPath()) {
		t.Errorf("expected daemon.log path in hint, got:\n%s", out)
	}
}

func TestWriteDaemonStartError_UnknownErrorNoHint(t *testing.T) {
	err := errors.New("something else")
	var buf bytes.Buffer
	writeDaemonStartError(&buf, err)
	out := buf.String()
	if strings.Contains(out, "Next steps") {
		t.Errorf("did not expect hint for unclassified error, got:\n%s", out)
	}
	if !strings.HasPrefix(out, "Error: something else") {
		t.Errorf("expected Error line, got:\n%s", out)
	}
}

func TestWriteDaemonStartError_PortConflictShowsCopyableRecovery(t *testing.T) {
	err := fmt.Errorf("starting daemon: %w", &daemon.PortConflictError{
		Port:          19800,
		PID:           123,
		SuggestedPort: 29800,
		Err:           errors.New("bind"),
	})
	var buf bytes.Buffer
	writeDaemonStartError(&buf, err)
	out := buf.String()
	for _, want := range []string{"held by pid 123", "ORBIT_DASHBOARD_PORT=29800", "orbit up"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
