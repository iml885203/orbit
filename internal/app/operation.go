package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/instance"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/spf13/cobra"
)

func operationCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "operation <id>",
		Short: "Read a recorded lifecycle operation without repeating it",
		Long: "Read one accepted lifecycle operation from the selected daemon/instance.\n" +
			"A successful query can return a failed operation; inspect its state and cleanup.\n" +
			"History is bounded and disappears with the daemon. Missing history does not prove success.\n" +
			"This command never starts a daemon or retries the original operation.",
		Args: cobra.ExactArgs(1),
		RunE: runOperation,
	}
}

func runOperation(cmd *cobra.Command, args []string) error {
	op, err := daemon.NewClient(daemon.DefaultSocketPath()).WithContext(cmd.Context()).Operation(args[0])
	if err != nil {
		return operationQueryError{cause: fmt.Errorf("querying operation %q: %w", args[0], err)}
	}
	if op.ID != args[0] || (instance.CurrentName() != "" && op.Instance != instance.CurrentName()) {
		return operationQueryError{cause: errors.New("daemon returned an operation with a different identity or instance")}
	}
	switch op.State {
	case "accepted", "running", "succeeded", "failed":
	default:
		return operationQueryError{cause: fmt.Errorf("daemon returned unknown operation state %q", op.State)}
	}
	actions := []cli.JSONAction{{Command: operationDiagnosticCommand([]string{"status"}, true), Reason: "Inspect current resource state separately from this recorded operation."}}
	if op.State == "accepted" || op.State == "running" {
		actions = operationObservationActions(op.ID)
	}
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(cmd.OutOrStdout(), commandString(), op, actions)
	}
	lines := []string{
		"Recorded operation: " + op.ID,
		"Kind: " + op.Kind,
		"State: " + op.State,
		"Phase: " + op.Phase,
		fmt.Sprintf("Context: epoch %d, generation %d", op.Epoch, op.Generation),
		"Config: " + op.ConfigPath,
		"Resources: " + strings.Join(op.Resources, ", "),
		"Accepted: " + op.AcceptedAt.Format(time.RFC3339Nano),
	}
	if op.Instance != "" {
		lines = append(lines, "Instance: "+op.Instance)
	}
	if op.StartedAt != nil {
		lines = append(lines, "Started: "+op.StartedAt.Format(time.RFC3339Nano))
	}
	if op.FinishedAt != nil {
		lines = append(lines, "Finished: "+op.FinishedAt.Format(time.RFC3339Nano))
	}
	if op.Cleanup != "" {
		lines = append(lines, "Cleanup: "+op.Cleanup)
	}
	if op.Cleanup == "unavailable" {
		lines = append(lines, "Orphan cleanup was not verified; stopped resources do not prove the namespace is clean.")
	}
	for _, warning := range op.Warnings {
		lines = append(lines, "Warning: "+warning)
	}
	if op.Error != "" {
		lines = append(lines, "Operation error ("+op.Code+"): "+op.Error)
	}
	lines = append(lines, "Query succeeded; the recorded state above is the original operation's outcome.")
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n")); err != nil {
		return fmt.Errorf("writing operation result: %w", err)
	}
	return nil
}

func operationDiagnosticCommand(args []string, jsonOutput bool) string {
	parts := []string{"orbit"}
	if name := instance.CurrentName(); name != "" {
		parts = append(parts, "--instance", name)
	}
	if configFlagGiven && configFile != "" {
		parts = append(parts, "--config", configFile)
	}
	parts = append(parts, args...)
	if jsonOutput {
		parts = append(parts, "--json")
	}
	for i := 1; i < len(parts); i++ {
		parts[i] = shellquote.Quote(parts[i])
	}
	return strings.Join(parts, " ")
}

func operationObservationActions(id string) []cli.JSONAction {
	return []cli.JSONAction{{
		Command: operationDiagnosticCommand([]string{"operation", id}, true),
		Reason:  "Read the accepted operation's result without repeating its mutation.",
	}}
}

type operationQueryError struct{ cause error }

func (e operationQueryError) Error() string { return e.cause.Error() }
func (e operationQueryError) Unwrap() error { return e.cause }
func (e operationQueryError) ErrorCode() string {
	var coded interface{ ErrorCode() string }
	if errors.As(e.cause, &coded) {
		return coded.ErrorCode()
	}
	if errors.Is(e.cause, daemon.ErrDaemonUnreachable) {
		return "daemon_unreachable"
	}
	return "operation_unavailable"
}
func (e operationQueryError) CLIJSONHint() string {
	return "Keep the operation ID and inspect current status before another mutation. Missing history or a restarted daemon cannot establish the original result."
}
func (e operationQueryError) CLIHumanContext() string { return e.CLIJSONHint() }
func (e operationQueryError) CLIHumanNextCommand() string {
	return operationDiagnosticCommand([]string{"status"}, false)
}
func (e operationQueryError) CLIJSONReplacementActions() []cli.JSONAction {
	return []cli.JSONAction{{Command: operationDiagnosticCommand([]string{"status"}, true), Reason: "Inspect the current runtime; the recorded operation result is unavailable."}}
}
