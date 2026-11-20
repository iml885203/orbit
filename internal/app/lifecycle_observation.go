package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
)

type lifecycleObservationResult struct {
	Status    *daemon.StatusResponse
	Operation *daemon.LifecycleOperation
}

type lifecycleObservationError struct{ code, message string }

func (e lifecycleObservationError) Error() string     { return e.message }
func (e lifecycleObservationError) ErrorCode() string { return e.code }
func (e lifecycleObservationError) CLIJSONHint() string {
	return "Preserve the operation ID and inspect 'orbit status --json' before another mutation; accepted daemon work may continue."
}

type lifecycleOperationObservation struct {
	operation *daemon.LifecycleOperation
	status    *daemon.StatusResponse
	err       error
}

func lifecycleObservationProgress(ctx context.Context, started time.Time) *lifecycleProgress {
	var out io.Writer = os.Stdout
	if cli.JSONOutput {
		out = os.Stderr
	}
	ticker := time.NewTicker(progressCheckInterval)
	return newLifecycleProgressWithTicker(ctx, out, started, 5*time.Second, ticker.C, ticker.Stop)
}

func announceLifecycleAcceptance(response *daemon.APIResponse, progress *lifecycleProgress) {
	message := response.Message
	if message == "" {
		message = "lifecycle request accepted"
	}
	if response.OperationID != "" {
		message += " (operation " + response.OperationID + ")"
	}
	progress.Phase(lifecyclePhase("accepted: " + message))
}

func observeLifecycleOperation(ctx context.Context, client *daemon.Client, id string, observations chan<- lifecycleOperationObservation) {
	for {
		requestContext, cancel := context.WithTimeout(ctx, lifecycleStatusRequestTimeout)
		operation, err := client.WithContext(requestContext).Operation(id)
		cancel()
		observation := lifecycleOperationObservation{operation: operation, err: err}
		if err == nil && operation.State != "failed" {
			requestContext, cancel = context.WithTimeout(ctx, lifecycleStatusRequestTimeout)
			observation.status, err = client.WithContext(requestContext).Status()
			cancel()
			if operation.State == "succeeded" {
				observation.err = err
			}
		}
		select {
		case observations <- observation:
		case <-ctx.Done():
			return
		}
		if observation.err != nil || (operation != nil && (operation.State == "failed" || operation.State == "succeeded")) {
			return
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

func validateLifecycleOperation(operation *daemon.LifecycleOperation, response *daemon.APIResponse, before *daemon.StatusResponse, previous *daemon.LifecycleOperation) error {
	if operation == nil {
		return lifecycleObservationError{"operation_invalid", "daemon returned no lifecycle operation"}
	}
	if operation.ID != response.OperationID || operation.Epoch != before.Epoch || operation.Generation != before.Generation || operation.ConfigPath != before.ConfigPath || operation.Instance != before.Instance {
		return lifecycleObservationError{"operation_target_changed", fmt.Sprintf("operation %s no longer matches the accepted environment/instance; inspect current status before another mutation", response.OperationID)}
	}
	if previous != nil && (operation.Generation != previous.Generation || operation.Kind != previous.Kind || !slices.Equal(operation.Resources, previous.Resources)) {
		return lifecycleObservationError{"operation_target_changed", fmt.Sprintf("operation %s changed its recorded target", response.OperationID)}
	}
	if len(response.AffectedResources) > 0 {
		expected := append([]string{}, response.AffectedResources...)
		actual := append([]string{}, operation.Resources...)
		sort.Strings(expected)
		sort.Strings(actual)
		if !slices.Equal(expected, actual) {
			return lifecycleObservationError{"operation_target_changed", fmt.Sprintf("operation %s changed its accepted resources", response.OperationID)}
		}
	}
	switch operation.State {
	case "accepted", "running", "succeeded", "failed":
		return nil
	}
	return lifecycleObservationError{"operation_invalid", fmt.Sprintf("operation %s has unknown state %q", response.OperationID, operation.State)}
}

func waitForAcceptedLifecycle(ctx context.Context, client *daemon.Client, before *daemon.StatusResponse, response *daemon.APIResponse, names []string, progress *lifecycleProgress, started time.Time) (lifecycleObservationResult, error) {
	result := lifecycleObservationResult{}
	if response.OperationID == "" {
		progress.Phase("waiting for legacy daemon state; explicit operation result unavailable")
		status, err := waitForLifecycleJSONTargetContextProgress(ctx, client.WithContext(ctx), names, "stopped", nil, true, progress, before)
		result.Status = status
		return result, err
	}
	observations := make(chan lifecycleOperationObservation, 1)
	ctx, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		observeLifecycleOperation(ctx, client, response.OperationID, observations)
	}()
	defer func() { cancel(); <-finished }()
	var renderer progressRenderer
	if !cli.JSONOutput {
		renderer = newAppendRenderer(os.Stdout)
		defer func() { renderer.finalize(result.Operation != nil && result.Operation.State == "succeeded") }()
	}
	snapshots := map[string]progressSnapshot{}
	var observedStatus *daemon.StatusResponse
	frame := time.NewTicker(100 * time.Millisecond)
	defer frame.Stop()
	frameIndex := 0
	for {
		select {
		case <-ctx.Done():
			return result, lifecycleObservationContextError(ctx, response.OperationID, started)
		case observation := <-observations:
			if ctx.Err() != nil {
				return result, lifecycleObservationContextError(ctx, response.OperationID, started)
			}
			if observation.operation != nil {
				if err := validateLifecycleOperation(observation.operation, response, before, result.Operation); err != nil {
					return result, err
				}
				result.Operation = observation.operation
			}
			if observation.err != nil {
				return result, fmt.Errorf("operation %s observation unavailable; accepted work may continue: %w", response.OperationID, observation.err)
			}
			if observation.operation == nil {
				return result, lifecycleObservationError{"operation_invalid", "daemon returned no operation result"}
			}
			if observation.status != nil {
				status := observation.status
				if err := validateLifecycleStatusTarget(status, before); err != nil {
					return result, err
				}
				result.Status = status
				observedStatus = status
			}

			operation := result.Operation
			if operation.State == "failed" {
				code := operation.Code
				if code == "" {
					code = "lifecycle_failed"
				}
				return result, lifecycleObservationError{code, fmt.Sprintf("operation %s failed: %s", operation.ID, operation.Error)}
			}
			progress.Phase(lifecyclePhase("operation " + operation.ID + ": " + operation.Phase))
			if operation.State == "succeeded" {
				return result, nil
			}
		case now := <-frame.C:
			if observedStatus == nil {
				continue
			}
			frameIndex++
			resources := make([]daemon.ResourceStatus, 0, len(names))
			for i := range observedStatus.Resources {
				resource := &observedStatus.Resources[i]
				if len(names) == 0 || slices.Contains(names, resource.Name) {
					resources = append(resources, *resource)
				}
			}
			next := nextSnapshots(snapshots, resources, now)
			progress.ObserveResources(resources, now)
			for _, event := range diffProgress(snapshots, next, now) {
				if event.kind == eventTransition {
					line := fmt.Sprintf("%s %s (observation elapsed %s)", event.name, event.to, fmtDur(now.Sub(started)))
					if renderer != nil {
						renderer.commit("  " + line)
					} else {
						fmt.Fprintln(os.Stderr, "⋯ "+line)
					}
				}
			}
			if renderer != nil {
				renderer.render(next, nil, now, frameIndex)
			}
			snapshots = next
		}
	}
}

func validateLifecycleStatusTarget(status, before *daemon.StatusResponse) error {
	if before != nil && (status.Epoch != before.Epoch || status.Generation != before.Generation || status.ConfigPath != before.ConfigPath || status.Instance != before.Instance) {
		return lifecycleObservationError{"operation_target_changed", "observed status no longer matches accepted lifecycle environment/instance"}
	}
	return nil
}

func lifecycleObservationContextError(ctx context.Context, id string, started time.Time) error {
	scope := "lifecycle request"
	recovery := operationDiagnosticCommand([]string{"status"}, true)
	if id != "" {
		scope = "operation " + id
		recovery = operationDiagnosticCommand([]string{"operation", id}, true)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return cli.NewTimeoutError(fmt.Sprintf("timeout observing %s after %s; accepted daemon work may continue — inspect '%s' before another mutation", scope, fmtDur(time.Since(started)), recovery))
	}
	return cli.NewCanceledError(fmt.Sprintf("detached from %s; accepted daemon work may continue — inspect '%s'", scope, recovery))
}

func finishLifecycleObservation(result lifecycleObservationResult, response *daemon.APIResponse, started time.Time, err error, options lifecycleJSONOptions, actions []cli.JSONAction) error {
	options.FinalStatus = result.Status
	options.OperationID = response.OperationID
	options.ElapsedMS = time.Since(started).Milliseconds()
	if result.Operation != nil {
		options.OperationState = result.Operation.State
		options.Cleanup = result.Operation.Cleanup
		options.Warnings = append([]string(nil), result.Operation.Warnings...)
		if err == nil && options.OperationState == "succeeded" && options.Cleanup == "unavailable" {
			options.Message = "Configured resources stopped; orphan container cleanup remains unverified. Inspect orbit doctor and orbit status --json before assuming cleanup is complete."
		}
	} else {
		options.OperationState = "unknown"
	}
	for _, warning := range options.Warnings {
		fmt.Fprintln(os.Stderr, "Warning: "+warning)
	}
	if err != nil {
		options.Message = err.Error()
		if response.OperationID != "" && (errors.Is(err, cli.ErrTimeout) || errors.Is(err, cli.ErrCanceled)) {
			err = cli.WithJSONReplacementActions(err, operationObservationActions(response.OperationID))
		}
		if !cli.JSONOutput && errors.Is(err, cli.ErrCanceled) {
			fmt.Println(options.Message)
			return nil
		}
		if cli.JSONOutput {
			if writeErr := cli.WriteJSONFailure(os.Stdout, commandString(), buildLifecycleJSONData(options), err, lifecycleRecommendedActions(options.RequestedResources)); writeErr != nil {
				return writeErr
			}
			return errCLIJSONAlreadyRendered{err: err}
		}
		return err
	}
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildLifecycleJSONData(options), actions)
	}
	fmt.Printf("%s (elapsed %s)\n", strings.TrimSpace(options.Message), fmtDur(time.Since(started)))
	return nil
}
