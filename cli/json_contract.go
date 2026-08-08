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
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

// SchemaVersion tags every envelope and JSON event the CLI emits.
const SchemaVersion = "orbit.cli.v1"

type JSONEnvelope struct {
	SchemaVersion      string       `json:"schema_version"`
	OK                 bool         `json:"ok"`
	Command            string       `json:"command"`
	Instance           string       `json:"instance,omitempty"`
	Data               any          `json:"data,omitempty"`
	Error              *JSONError   `json:"error,omitempty"`
	Notices            []JSONNotice `json:"notices,omitempty"`
	RecommendedActions []JSONAction `json:"recommended_actions,omitempty"`
}

type JSONNotice struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
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
	var replacement interface{ CLIJSONReplacementActions() []JSONAction }
	if errors.As(err, &replacement) {
		actions = replacement.CLIJSONReplacementActions()
		classified.NextCommand = ""
		if len(actions) > 0 {
			classified.NextCommand = actions[0].Command
		}
	} else {
		actions = MergeActions(recommendedActionsForError(classified), actions)
		var additive interface{ CLIJSONActions() []JSONAction }
		if errors.As(err, &additive) {
			actions = MergeActions(actions, additive.CLIJSONActions())
		}
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
	payload.Notices = takeJSONNotices()
	payload.Instance = os.Getenv("ORBIT_INSTANCE")
	if payload.Instance != "" {
		for i := range payload.RecommendedActions {
			payload.RecommendedActions[i].Command = instanceTargetedCommand(payload.RecommendedActions[i].Command, payload.Instance)
		}
		if payload.Error != nil {
			payload.Error.NextCommand = instanceTargetedCommand(payload.Error.NextCommand, payload.Instance)
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func instanceTargetedCommand(command, name string) string {
	if command == "" || !strings.HasPrefix(command, "orbit ") || strings.Contains(command, " --instance ") {
		return command
	}
	return "orbit --instance " + name + " " + strings.TrimPrefix(command, "orbit ")
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
			Message:     "Orbit is not running.",
			Hint:        "Run 'orbit up' to start the selected environment.",
			Retryable:   true,
			NextCommand: "orbit up --json",
		}
	}
	var schemaVersion *config.SchemaVersionMismatchError
	if errors.As(err, &schemaVersion) {
		switch schemaVersion.Kind {
		case config.SchemaVersionOlder:
			if !IsManagedEnvironmentPath(schemaVersion.Path) {
				return JSONError{
					Code:      "environment_schema_outdated",
					Message:   schemaVersion.Error(),
					Hint:      "Migrate this project-local environment file to the supported schema.",
					Retryable: false,
				}
			}
			return JSONError{
				Code:        "environment_schema_outdated",
				Message:     schemaVersion.Error(),
				Hint:        "Refresh the shared environment files.",
				Retryable:   true,
				NextCommand: "orbit source sync --json",
			}
		case config.SchemaVersionNewer:
			return JSONError{
				Code:        "environment_schema_newer",
				Message:     schemaVersion.Error(),
				Hint:        "Update Orbit to a version that supports this environment schema.",
				Retryable:   true,
				NextCommand: "orbit update --json",
			}
		default:
			return JSONError{
				Code:      "invalid_environment_schema",
				Message:   schemaVersion.Error(),
				Hint:      "Set the required numeric environment schema version before retrying.",
				Retryable: false,
			}
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
	var resourcePortConflict *ResourcePortConflictError
	if errors.As(err, &resourcePortConflict) {
		return JSONError{
			Code:        "resource_port_conflict",
			Message:     msg,
			Hint:        "Stop the reported port owner or change this resource's host port in the environment, then run 'orbit up' again.",
			Retryable:   true,
			NextCommand: resourcePortConflict.InspectCommand,
		}
	}
	var configMismatch *daemon.ConfigMismatchError
	if errors.As(err, &configMismatch) {
		hint := "Restart the daemon with the selected config, or explicitly select the running daemon config."
		if configMismatch.Running == "" {
			hint = "Restart the daemon with the selected config so Orbit can verify which environment receives the command."
		}
		return JSONError{
			Code:        "env_mismatch",
			Message:     msg,
			Hint:        hint,
			Retryable:   true,
			NextCommand: "orbit daemon restart -c " + strconv.Quote(configMismatch.Requested),
		}
	}
	var configStale *daemon.ConfigStaleError
	if errors.As(err, &configStale) {
		return JSONError{
			Code:        "environment_changed",
			Message:     msg,
			Hint:        "Apply the selected environment changes; Orbit will restore the resources that were running.",
			Retryable:   true,
			NextCommand: "orbit env apply --json",
		}
	}
	var updateRequired *daemon.UpdateRequiredError
	if errors.As(err, &updateRequired) {
		nextCommand := updateRequired.RestartJSONCommand
		if nextCommand == "" {
			nextCommand = "orbit daemon restart --json"
		}
		return JSONError{
			Code:        "orbit_update_pending",
			Message:     msg,
			Hint:        "Restart Orbit once to run the installed version before operating resources.",
			Retryable:   true,
			NextCommand: nextCommand,
		}
	}
	var codedErr interface{ ErrorCode() string }
	if errors.As(err, &codedErr) {
		switch codedErr.ErrorCode() {
		case "setup_required":
			hint := "Set up Orbit before running this command."
			var hintedErr interface{ CLIJSONHint() string }
			if errors.As(err, &hintedErr) && hintedErr.CLIJSONHint() != "" {
				hint = hintedErr.CLIJSONHint()
			}
			return JSONError{
				Code:        "setup_required",
				Message:     msg,
				Hint:        hint,
				Retryable:   true,
				NextCommand: "orbit init --yes --json",
			}
		case "environment_selection_required":
			hint := "Select one of the available environments reported by Orbit."
			var hintedErr interface{ CLIJSONHint() string }
			if errors.As(err, &hintedErr) && hintedErr.CLIJSONHint() != "" {
				hint = hintedErr.CLIJSONHint()
			}
			return JSONError{
				Code:      "environment_selection_required",
				Message:   msg,
				Hint:      hint,
				Retryable: true,
			}
		case "project_context_inactive":
			hint := "Run orbit up from this project to switch the active environment."
			var hintedErr interface{ CLIJSONHint() string }
			if errors.As(err, &hintedErr) && hintedErr.CLIJSONHint() != "" {
				hint = hintedErr.CLIJSONHint()
			}
			return JSONError{
				Code:      "project_context_inactive",
				Message:   msg,
				Hint:      hint,
				Retryable: true,
			}
		case "unknown_group":
			return JSONError{
				Code:      "invalid_argument",
				Message:   msg,
				Hint:      "Choose one of the available groups reported by Orbit, then retry.",
				Retryable: false,
			}
		case "invalid_argument":
			hint := "Correct the conflicting or unknown command selection, then retry."
			var hintedErr interface{ CLIJSONHint() string }
			if errors.As(err, &hintedErr) && hintedErr.CLIJSONHint() != "" {
				hint = hintedErr.CLIJSONHint()
			}
			return JSONError{
				Code:      "invalid_argument",
				Message:   msg,
				Hint:      hint,
				Retryable: false,
			}
		case "unknown_resource":
			hint := "Run 'orbit status' to list configured resources."
			var hintedErr interface{ CLIJSONHint() string }
			if errors.As(err, &hintedErr) && hintedErr.CLIJSONHint() != "" {
				hint = hintedErr.CLIJSONHint()
			}
			return JSONError{
				Code:        "unknown_resource",
				Message:     msg,
				Hint:        hint,
				Retryable:   false,
				NextCommand: "orbit status --json",
			}
		default:
			var hintedErr interface{ CLIJSONHint() string }
			if errors.As(err, &hintedErr) && hintedErr.CLIJSONHint() != "" {
				return JSONError{
					Code:      codedErr.ErrorCode(),
					Message:   msg,
					Hint:      hintedErr.CLIJSONHint(),
					Retryable: false,
				}
			}
		}
	}
	switch {
	case errors.Is(err, ErrUnknownResource):
		return JSONError{
			Code:        "unknown_resource",
			Message:     msg,
			Hint:        "Run 'orbit status' to list configured resources.",
			Retryable:   false,
			NextCommand: "orbit status --json",
		}
	// A daemon that accepted the connection but ran out the clock is busy,
	// not absent — the same recovery as any other timeout, and pointedly not
	// daemon_unreachable's "run orbit up".
	case errors.Is(err, ErrTimeout), errors.Is(err, daemon.ErrDaemonTimeout):
		return JSONError{
			Code:        "timeout",
			Message:     msg,
			Hint:        "Inspect resource state and logs before retrying.",
			Retryable:   true,
			NextCommand: "orbit status --json",
		}
	case errors.Is(err, ErrNotConfigured):
		hint := "This feature requires configuration the active env does not provide."
		var hintedErr interface{ CLIJSONHint() string }
		if errors.As(err, &hintedErr) && hintedErr.CLIJSONHint() != "" {
			hint = hintedErr.CLIJSONHint()
		}
		return JSONError{
			Code:    "not_configured",
			Message: msg,
			// No docs pointer here: the message itself (owned by the daemon,
			// e.g. ErrMsgDBNotConfigured) already carries one.
			Hint:      hint,
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
	case errors.Is(err, ErrLogsUnavailable):
		return JSONError{
			Code:      "logs_unavailable",
			Message:   msg,
			Hint:      "Follow the recommended recovery action; there is no process output to inspect yet.",
			Retryable: true,
		}
	case errors.Is(err, ErrServiceWorkingDir):
		return JSONError{
			Code:      "service_working_directory_missing",
			Message:   msg,
			Hint:      "Resolve the path variable or correct the service working directory before starting resources.",
			Retryable: true,
		}
	case errors.Is(err, ErrEnvRepoAccess):
		return JSONError{
			Code:        "env_repo_access",
			Message:     msg,
			Hint:        "Verify Git access. For a private GitHub repository, run 'gh auth login' and 'gh auth setup-git', then retry.",
			Retryable:   true,
			NextCommand: "orbit source sync --json",
		}
	case errors.Is(err, ErrEnvRepoUnavailable):
		return JSONError{
			Code:      "env_repo_unavailable",
			Message:   msg,
			Hint:      "Verify the repository owner and name. If the URL is correct and the repository is private, authenticate Git before retrying.",
			Retryable: true,
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
	case errors.Is(err, ErrInvalidArgument):
		return JSONError{
			Code:      "invalid_argument",
			Message:   msg,
			Hint:      "Correct the conflicting or unknown command selection, then retry.",
			Retryable: false,
		}
	case errors.Is(err, ErrConfirmationRequired):
		return JSONError{
			Code:      "confirmation_required",
			Message:   msg,
			Hint:      "Rerun with --yes to confirm the destructive step.",
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

// IsManagedEnvironmentPath reports whether path belongs to Orbit's synced
// environment repository. Project-local files require a documented schema
// edit; recommending env sync for them creates a recovery loop.
func IsManagedEnvironmentPath(path string) bool {
	root := filepath.Join(daemon.OrbitDir(), "envs")
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func recommendedActionsForError(err JSONError) []JSONAction {
	if err.Code == "json_unsupported_destructive_command" {
		return nil
	}
	if err.Code == "env_repo_access" {
		return []JSONAction{{
			Command:     "orbit source sync --json",
			Reason:      "Retry after restoring Git access to the environment repository.",
			Destructive: false,
		}}
	}
	if err.Code == "env_repo_unavailable" {
		return nil
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
	if err.Code == "invalid_argument" {
		return nil
	}
	if err.Code == "not_configured" {
		return nil
	}
	if err.Code == "environment_selection_required" {
		return nil
	}
	if err.Code == "setup_required" {
		return []JSONAction{{
			Command:     "orbit init --yes --json",
			Reason:      "Set up Orbit before running environment commands.",
			Destructive: false,
		}}
	}
	if err.Code == "project_context_inactive" {
		return nil
	}
	if err.Code == "orbit_update_pending" {
		return []JSONAction{{
			Command:     err.NextCommand,
			Reason:      "Restart Orbit to run the installed version.",
			Destructive: false,
		}}
	}
	if err.Code == "daemon_unreachable" {
		return []JSONAction{{
			Command:     "orbit up --json",
			Reason:      "Start the selected environment.",
			Destructive: false,
		}}
	}
	if err.Code == "environment_schema_outdated" {
		if err.NextCommand == "" {
			return nil
		}
		return []JSONAction{{
			Command:     err.NextCommand,
			Reason:      "Refresh the shared environment files.",
			Destructive: false,
		}}
	}
	if err.Code == "environment_schema_newer" {
		return []JSONAction{{
			Command:     "orbit update --json",
			Reason:      "Update Orbit to support this environment schema.",
			Destructive: false,
		}}
	}
	if err.Code == "invalid_environment_schema" {
		return nil
	}
	if err.Code == "resource_port_conflict" {
		if err.NextCommand == "" {
			return nil
		}
		return []JSONAction{{
			Command:     err.NextCommand,
			Reason:      "Inspect the process currently using the required port.",
			Destructive: false,
		}}
	}
	if err.Code == "service_start_failed" || err.Code == "dependency_blocked" || err.Code == "timeout" {
		return []JSONAction{StatusAction()}
	}
	if err.Code == "logs_unavailable" {
		return nil
	}
	if err.Code == "service_working_directory_missing" {
		return nil
	}
	if err.Code == "env_mismatch" {
		return []JSONAction{{
			Command:     err.NextCommand,
			Reason:      "Restart the daemon with the selected environment.",
			Destructive: false,
		}}
	}
	actions := []JSONAction{
		{Command: "orbit status --json", Reason: "Inspect the latest daemon and resource state.", Destructive: false},
	}
	if err.Code != "unknown_resource" {
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

type replacementActionError struct {
	err     error
	actions []JSONAction
}

func (e replacementActionError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e replacementActionError) Unwrap() error {
	return e.err
}

func (e replacementActionError) CLIJSONReplacementActions() []JSONAction {
	return e.actions
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

func WithJSONReplacementActions(err error, actions []JSONAction) error {
	if err == nil {
		return nil
	}
	return replacementActionError{err: err, actions: actions}
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
		Reason:      "Inspect the latest daemon and resource state.",
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
