package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/daemonsrv"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/spf13/cobra"
)

func runLogs(cmd *cobra.Command, args []string) error {
	if logLines < 0 {
		return cli.NewInvalidArgumentError(fmt.Sprintf("--lines/--tail must be zero or more, got %d", logLines))
	}
	names := make([]string, 0, len(args))
	seen := make(map[string]bool, len(args))
	for _, name := range args {
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	retryCommand := func(suggestion string) string {
		command := "orbit logs " + suggestion
		if follow {
			command += " --follow"
		}
		return command
	}
	if err := validateConfiguredCommandResources(configFile, names, func(_ int, suggestion string) string { return retryCommand(suggestion) }); err != nil {
		return err
	}
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return cli.NewOrbitNotRunningError()
	}
	ctx := cmd.Context()
	client = client.WithContext(ctx)
	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	for _, name := range names {
		if lifecycleResourceStatus(status, name) == nil {
			return newResourceNameError(status, name, retryCommand)
		}
	}
	if follow {
		return followResourceLogs(ctx, client, cmd.OutOrStdout(), names)
	}
	return showResourceLogs(client, cmd.OutOrStdout(), names, status)
}

type multiLogsJSONData struct {
	Resources []logsJSONData `json:"resources"`
}

func showResourceLogs(client *daemon.Client, out io.Writer, names []string, status *daemon.StatusResponse) error {
	snapshots := make([]logsJSONData, 0, len(names))
	actions := []cli.JSONAction{}
	resources := make([]*daemon.ResourceStatus, 0, len(names))
	for _, name := range names {
		resource := lifecycleResourceStatus(status, name)
		resp, err := client.Logs(name, logLines)
		if err != nil {
			return fmt.Errorf("logs for %s failed: %w", name, err)
		}
		if (resp == nil || len(resp.Lines) == 0) && !resource.LogsAvailable &&
			(resource.State == "degraded" || resource.State == "pending" || resource.State == "stopped") {
			err := cli.NewLogsUnavailableError("no logs for " + name + ": the resource did not start")
			return cli.WithJSONActions(err, noLogsRecoveryActions(resource))
		}
		snapshots = append(snapshots, buildLogsJSONData(name, logLines, resp))
		resources = append(resources, resource)
	}
	if latest, err := client.Status(); err == nil {
		for i, name := range names {
			if current := lifecycleResourceStatus(latest, name); current != nil {
				resources[i] = current
			}
		}
	}
	for i, resource := range resources {
		actions = append(actions, logsRecoveryActions(resource, projectDependencySetupCommand(names[i]))...)
	}
	if cli.JSONOutput {
		if len(names) == 1 {
			return cli.WriteJSONSuccess(out, commandString(), snapshots[0], actions)
		}
		return cli.WriteJSONSuccess(out, commandString(), multiLogsJSONData{Resources: snapshots}, actions)
	}
	for i, snapshot := range snapshots {
		for _, line := range snapshot.Lines {
			if err := writeResourceLogLine(out, len(names) > 1, names[i], line); err != nil {
				return err
			}
		}
		resource := resources[i]
		if resource.FailureKind == string(engine.FailureKindHealth) {
			for _, line := range []string{"Health check is still failing: " + resource.StateReason, "Orbit will recover automatically when the endpoint is healthy; restarting only retries the same startup."} {
				if err := writeResourceLogLine(out, len(names) > 1, names[i], line); err != nil {
					return err
				}
			}
		}
	}
	if len(actions) == 1 {
		_, err := fmt.Fprintln(out, "Next: "+strings.TrimSuffix(actions[0].Command, " --json"))
		return err
	}
	return nil
}

func writeResourceLogLine(out io.Writer, attributed bool, name, line string) error {
	if attributed {
		for _, part := range strings.Split(line, "\n") {
			if _, err := fmt.Fprintf(out, "[%s] %s\n", name, part); err != nil {
				return fmt.Errorf("writing logs for %s: %w", name, err)
			}
		}
		return nil
	}
	_, err := fmt.Fprintln(out, line)
	return err
}

type resourceLogDelivery struct {
	resource string
	line     string
	done     bool
	err      error
}

func followResourceLogs(parent context.Context, client *daemon.Client, out io.Writer, names []string) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	deliveries := make(chan resourceLogDelivery)
	client = client.WithContext(ctx)
	if !cli.JSONOutput {
		if _, err := fmt.Fprintf(out, "Streaming logs for %s (Ctrl+C to stop)...\n", strings.Join(names, ", ")); err != nil {
			return err
		}
	}
	for _, name := range names {
		go func(name string) {
			err := client.StreamLogs(name, logLines, func(line string) {
				select {
				case deliveries <- resourceLogDelivery{resource: name, line: line}:
				case <-ctx.Done():
				}
			})
			deliveries <- resourceLogDelivery{resource: name, done: true, err: err}
		}(name)
	}
	remaining := len(names)
	var failure error
	for remaining > 0 {
		delivery := <-deliveries
		if delivery.done {
			remaining--
		}
		if failure != nil || parent.Err() != nil {
			cancel()
			continue
		}
		switch {
		case delivery.done:
			failure = delivery.err
			if failure == nil {
				continue
			}
			failure = fmt.Errorf("log stream for %s: %w", delivery.resource, failure)
			if cli.JSONOutput {
				if err := writeLogJSONErrorEvent(out, delivery.resource, failure); err != nil {
					failure = fmt.Errorf("writing log stream error: %w", err)
				}
			}
		case cli.JSONOutput:
			failure = writeLogJSONEvent(out, delivery.resource, delivery.line)
		default:
			failure = writeResourceLogLine(out, len(names) > 1, delivery.resource, delivery.line)
		}
		if failure != nil {
			cancel()
		}
	}
	if failure != nil && cli.JSONOutput {
		return errCLIJSONAlreadyRendered{err: failure}
	}
	return failure
}

func projectDependencySetupCommand(resource string) string {
	cfg, err := config.Load(configFile)
	if err != nil {
		return ""
	}
	command, _ := daemonsrv.ProjectDependencySetupCommand(cfg, resource)
	return command
}

type logsJSONData struct {
	Resource       string   `json:"resource"`
	LinesRequested int      `json:"lines_requested"`
	Lines          []string `json:"lines"`
	Truncated      bool     `json:"truncated"`
}

type logJSONEvent struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	Resource      string `json:"resource"`
	Line          string `json:"line"`
}

type logJSONErrorEvent struct {
	SchemaVersion string        `json:"schema_version"`
	Type          string        `json:"type"`
	Resource      string        `json:"resource"`
	Error         cli.JSONError `json:"error"`
}

func buildLogsJSONData(resource string, requested int, resp *daemon.LogsResponse) logsJSONData {
	lines := []string{}
	if resp != nil && resp.Lines != nil {
		lines = resp.Lines
	}
	return logsJSONData{
		Resource:       resource,
		LinesRequested: requested,
		Lines:          lines,
		Truncated:      requested > 0 && len(lines) >= requested,
	}
}

func writeLogJSONEvent(w io.Writer, resource, line string) error {
	return json.NewEncoder(w).Encode(logJSONEvent{
		SchemaVersion: cli.SchemaVersion,
		Type:          "log",
		Resource:      resource,
		Line:          line,
	})
}

func writeLogJSONErrorEvent(w io.Writer, resource string, err error) error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return json.NewEncoder(w).Encode(logJSONErrorEvent{
		SchemaVersion: cli.SchemaVersion,
		Type:          "error",
		Resource:      resource,
		Error: cli.JSONError{
			Code:      "log_stream_error",
			Message:   msg,
			Hint:      "Retry the stream or inspect daemon state with 'orbit status --json'.",
			Retryable: true,
		},
	})
}
