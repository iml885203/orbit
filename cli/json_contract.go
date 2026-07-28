// Package cli owns the orbit CLI's shared output: the machine-readable
// --json contract (envelope, error classification, recommended actions,
// destructive-command guard) and the human terminal rendering (palette,
// state icons). It lives outside cmd/orbit so extension packages can
// emit the same output from their own commands.
package cli

import (
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strconv"

	"github.com/iml885203/orbit/daemon"
)

// SchemaVersion tags every envelope and JSON event the CLI emits.
const SchemaVersion = "orbit.cli.v1"

type JSONEnvelope struct {
	SchemaVersion      string       `json:"schema_version"`
	OK                 bool         `json:"ok"`
	Command            string       `json:"command"`
	Data               any          `json:"data,omitempty"`
	Error              *JSONError   `json:"error,omitempty"`
	RecommendedActions []JSONAction `json:"recommended_actions,omitempty"`
}

type JSONError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Hint        string `json:"hint,omitempty"`
	Retryable   bool   `json:"retryable"`
	NextCommand string `json:"next_command,omitempty"`
}

type JSONAction struct {
	Command     string `json:"command"`
	Reason      string `json:"reason"`
	Destructive bool   `json:"destructive"`
}

func WriteJSONSuccess(w io.Writer, command string, data any, actions []JSONAction) error {
	return writeJSON(w, JSONEnvelope{
		SchemaVersion:      SchemaVersion,
		OK:                 true,
		Command:            command,
		Data:               data,
		RecommendedActions: actions,
	})
}

func WriteJSONError(w io.Writer, command string, err error) error {
	return WriteJSONFailure(w, command, nil, err, nil)
}

// WriteJSONFailure preserves diagnostic data alongside a machine-classified
// failure so callers do not have to choose between useful evidence and a
// truthful exit result.
func WriteJSONFailure(w io.Writer, command string, data any, err error, actions []JSONAction) error {
	classified := classify(err)
	actions = MergeActions(recommendedActionsForError(classified), actions)
	if withActions, ok := err.(interface{ CLIJSONActions() []JSONAction }); ok {
		actions = MergeActions(actions, withActions.CLIJSONActions())
	}
	return writeJSON(w, JSONEnvelope{
		SchemaVersion:      SchemaVersion,
		OK:                 false,
		Command:            command,
		Data:               data,
		Error:              &classified,
		RecommendedActions: actions,
	})
}

func writeJSON(w io.Writer, payload JSONEnvelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func classify(err error) JSONError {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	var destructiveErr unsupportedDestructiveJSONCommandError
	if errors.As(err, &destructiveErr) {
		return JSONError{
			Code:      "json_unsupported_destructive_command",
			Message:   msg,
			Hint:      "Run the command without --json only after confirming local data loss is acceptable.",
			Retryable: false,
		}
	}
	if errors.Is(err, daemon.ErrDaemonUnreachable) {
		return JSONError{
			Code:        "daemon_unreachable",
			Message:     msg,
			Hint:        "Start Orbit with 'orbit up' or inspect daemon state with 'orbit daemon status --json'.",
			Retryable:   true,
			NextCommand: "orbit status --json",
		}
	}
	var portConflict *daemon.PortConflictError
	if errors.As(err, &portConflict) {
		nextCommand := ""
		if portConflict.SuggestedPort > 0 {
			if runtime.GOOS == "windows" {
				nextCommand = "$env:ORBIT_DASHBOARD_PORT=" + strconv.Itoa(portConflict.SuggestedPort) + "; orbit up"
			} else {
				nextCommand = "ORBIT_DASHBOARD_PORT=" + strconv.Itoa(portConflict.SuggestedPort) + " orbit up"
			}
		}
		return JSONError{
			Code:        "dashboard_port_conflict",
			Message:     msg,
			Hint:        "Use the suggested free dashboard port or stop the reported port owner.",
			Retryable:   true,
			NextCommand: nextCommand,
		}
	}
	var configMismatch *daemon.ConfigMismatchError
	if errors.As(err, &configMismatch) {
		return JSONError{
			Code:        "env_mismatch",
			Message:     msg,
			Hint:        "Restart the daemon with the selected config, or explicitly select the running daemon config.",
			Retryable:   true,
			NextCommand: "orbit daemon restart -c " + strconv.Quote(configMismatch.Requested),
		}
	}
	switch {
	case errors.Is(err, ErrUnknownService):
		return JSONError{
			Code:        "unknown_service",
			Message:     msg,
			Hint:        "Run 'orbit status --json' to list configured services.",
			Retryable:   false,
			NextCommand: "orbit status --json",
		}
	case errors.Is(err, ErrTimeout):
		return JSONError{
			Code:        "timeout",
			Message:     msg,
			Hint:        "Inspect service state and logs before retrying.",
			Retryable:   true,
			NextCommand: "orbit status --json",
		}
	case errors.Is(err, ErrNotConfigured):
		return JSONError{
			Code:    "not_configured",
			Message: msg,
			// No docs pointer here: the message itself (owned by the daemon,
			// e.g. ErrMsgDBNotConfigured) already carries one.
			Hint:      "This feature requires configuration the active env does not provide.",
			Retryable: false,
		}
	case errors.Is(err, ErrChecksFailed):
		return JSONError{
			Code:        "checks_failed",
			Message:     msg,
			Hint:        "Resolve the failed checks, then run doctor again.",
			Retryable:   true,
			NextCommand: "orbit doctor --json",
		}
	case errors.Is(err, ErrDependencyBlocked):
		return JSONError{
			Code:        "dependency_blocked",
			Message:     msg,
			Hint:        "Restore the unhealthy dependency, then retry the requested service.",
			Retryable:   true,
			NextCommand: "orbit status --json",
		}
	case errors.Is(err, ErrServiceStartFailed):
		return JSONError{
			Code:        "service_start_failed",
			Message:     msg,
			Hint:        "Fix the reported service error, then retry the service.",
			Retryable:   true,
			NextCommand: "orbit status --json",
		}
	case errors.Is(err, ErrEnvRepoAccess):
		return JSONError{
			Code:        "env_repo_access",
			Message:     msg,
			Hint:        "Verify Git access. For a private GitHub repository, run 'gh auth login' and 'gh auth setup-git', then retry.",
			Retryable:   true,
			NextCommand: "orbit env sync --json",
		}
	case errors.Is(err, ErrInitIncomplete):
		return JSONError{
			Code:        "init_incomplete",
			Message:     msg,
			Hint:        "Complete the reported setup step, then retry initialization.",
			Retryable:   true,
			NextCommand: "orbit init --yes --json",
		}
	case errors.Is(err, ErrInvalidEnvironment):
		return JSONError{
			Code:      "invalid_environment",
			Message:   msg,
			Hint:      "Fix the reported environment file, then retry the same command.",
			Retryable: true,
		}
	default:
		return JSONError{
			Code:        "command_failed",
			Message:     msg,
			Hint:        "Run 'orbit doctor --json' for setup diagnostics.",
			Retryable:   false,
			NextCommand: "orbit doctor --json",
		}
	}
}

func recommendedActionsForError(err JSONError) []JSONAction {
	if err.Code == "json_unsupported_destructive_command" {
		return nil
	}
	if err.Code == "env_repo_access" {
		return []JSONAction{{
			Command:     "orbit env sync --json",
			Reason:      "Retry after restoring Git access to the environment repository.",
			Destructive: false,
		}}
	}
	if err.Code == "init_incomplete" {
		return []JSONAction{{
			Command:     "orbit init --yes --json",
			Reason:      "Retry initialization after resolving the reported setup issue.",
			Destructive: false,
		}}
	}
	if err.Code == "invalid_environment" {
		return nil
	}
	if err.Code == "service_start_failed" || err.Code == "dependency_blocked" || err.Code == "timeout" {
		return []JSONAction{StatusAction()}
	}
	if err.Code == "env_mismatch" {
		return []JSONAction{{
			Command:     err.NextCommand,
			Reason:      "Restart the daemon with the selected environment.",
			Destructive: false,
		}}
	}
	actions := []JSONAction{
		{Command: "orbit status --json", Reason: "Inspect the latest daemon and service state.", Destructive: false},
	}
	if err.Code != "unknown_service" {
		actions = append(actions, JSONAction{
			Command:     "orbit doctor --json",
			Reason:      "Run environment diagnostics and collect actionable hints.",
			Destructive: false,
		})
	}
	if err.NextCommand != "" && err.NextCommand != "orbit status --json" && err.NextCommand != "orbit doctor --json" {
		actions = append(actions, JSONAction{
			Command:     err.NextCommand,
			Reason:      "Run the next command suggested by Orbit.",
			Destructive: false,
		})
	}
	return actions
}

type actionError struct {
	err     error
	actions []JSONAction
}

func (e actionError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e actionError) Unwrap() error {
	return e.err
}

func (e actionError) CLIJSONActions() []JSONAction {
	return e.actions
}

func WithJSONActions(err error, actions []JSONAction) error {
	if err == nil {
		return nil
	}
	return actionError{err: err, actions: actions}
}

func MergeActions(base, extra []JSONAction) []JSONAction {
	out := append([]JSONAction{}, base...)
	seen := make(map[string]bool, len(out)+len(extra))
	for _, action := range out {
		seen[action.Command] = true
	}
	for _, action := range extra {
		if seen[action.Command] {
			continue
		}
		out = append(out, action)
		seen[action.Command] = true
	}
	return out
}

func StatusAction() JSONAction {
	return JSONAction{
		Command:     "orbit status --json",
		Reason:      "Inspect the latest daemon and service state.",
		Destructive: false,
	}
}

func DoctorAction() JSONAction {
	return JSONAction{
		Command:     "orbit doctor --json",
		Reason:      "Run environment diagnostics and collect actionable hints.",
		Destructive: false,
	}
}
