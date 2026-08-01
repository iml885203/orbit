package app

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
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

func TestWriteDaemonStartError_SocketPathTooLongDoesNotSuggestRestart(t *testing.T) {
	err := fmt.Errorf("%w: %w", daemon.ErrSocketPathTooLong,
		errors.New("/very/long/path/orbit.sock is 119 bytes, over the 104-byte OS limit for unix sockets"))
	var buf bytes.Buffer
	writeDaemonStartError(&buf, err)
	out := buf.String()
	if !strings.Contains(out, "ORBIT_HOME") {
		t.Errorf("expected the ORBIT_HOME remedy, got:\n%s", out)
	}
	for _, uselessAction := range []string{"orbit daemon stop", "orbit daemon restart"} {
		if strings.Contains(out, uselessAction) {
			t.Errorf("hint offered %q, which cannot fix a path-length failure:\n%s", uselessAction, out)
		}
	}
}

func TestRenderDaemonStartError_SocketPathTooLongJSONMatchesHumanRemedy(t *testing.T) {
	origJSON := cli.JSONOutput
	t.Cleanup(func() { cli.JSONOutput = origJSON })
	cli.JSONOutput = true

	err := fmt.Errorf("%w: %w", daemon.ErrSocketPathTooLong,
		errors.New("/very/long/orbit.sock is 119 bytes, over the 104-byte OS limit for unix sockets"))
	rendered := renderDaemonStartError(err)

	var coded interface{ ErrorCode() string }
	if !errors.As(rendered, &coded) || coded.ErrorCode() != "socket_path_too_long" {
		t.Fatalf("error is not classified for agents: %#v", rendered)
	}
	var hinted interface{ CLIJSONHint() string }
	if !errors.As(rendered, &hinted) || !strings.Contains(hinted.CLIJSONHint(), "ORBIT_HOME") {
		t.Errorf("JSON hint omits the ORBIT_HOME remedy: %#v", rendered)
	}
	var actions interface{ CLIJSONReplacementActions() []cli.JSONAction }
	if errors.As(rendered, &actions) && len(actions.CLIJSONReplacementActions()) != 0 {
		t.Errorf("offered a recommended action that cannot fix the path")
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
