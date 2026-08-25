package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/spf13/cobra"
)

type environmentApplyResult struct {
	Applied              bool
	DaemonRunning        bool
	ChangesPending       bool
	PreviousPID          int
	PID                  int
	PreviouslyRunning    []string
	RestoredResources    []string
	StartedDependencies  []string
	UnavailableResources []string
	FinalStatus          *daemon.StatusResponse
	reconcileDispatched  bool
}

type environmentApplyJSONData struct {
	Operation            string                  `json:"operation"`
	Applied              bool                    `json:"applied"`
	DaemonRunning        bool                    `json:"daemon_running"`
	ChangesPending       bool                    `json:"changes_pending"`
	PreviousPID          int                     `json:"previous_pid,omitempty"`
	PID                  int                     `json:"pid,omitempty"`
	PreviouslyRunning    []string                `json:"previously_running"`
	RestoredResources    []string                `json:"restored_resources"`
	StartedDependencies  []string                `json:"started_dependencies"`
	UnavailableResources []string                `json:"unavailable_resources"`
	Resources            []daemon.ResourceStatus `json:"resources"`
}

func envApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply a previously deferred environment update",
		RunE:  runEnvApply,
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "maximum duration for the complete operation (default 5m)")
	return cmd
}

func runEnvApply(cmd *cobra.Command, _ []string) error {
	if !cli.JSONOutput {
		result, err := applyEnvironmentChanges(environmentApplyProgress())
		if err != nil {
			return err
		}
		printEnvironmentApplyResult(result)
		return nil
	}
	ctx, stopSignals := lifecycleSignalContext(cmd.Context())
	defer stopSignals()
	ctx, cancel := lifecycleOperationContext(ctx)
	defer cancel()
	result, err := applyEnvironmentChangesContext(ctx, environmentApplyProgress())
	if err != nil {
		err = lifecycleOperationError(ctx, err, result.reconcileDispatched)
		if cli.JSONOutput {
			failure := cli.WithJSONReplacementActions(err, []cli.JSONAction{cli.StatusAction()})
			if writeErr := cli.WriteJSONFailure(os.Stdout, commandString(), buildEnvironmentApplyJSONData(result), failure, nil); writeErr != nil {
				return writeErr
			}
			return errCLIJSONAlreadyRendered{err: failure}
		}
		return err
	}
	return cli.WriteJSONSuccess(
		os.Stdout,
		commandString(),
		buildEnvironmentApplyJSONData(result),
		environmentApplyRecommendedActions(result),
	)
}

func applyEnvironmentChanges(report func(string)) (environmentApplyResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), effectiveTimeout(timeout))
	defer cancel()
	return applyEnvironmentChangesContext(ctx, report)
}

func applyEnvironmentChangesContext(ctx context.Context, report func(string)) (environmentApplyResult, error) {
	return applyEnvironmentChangesWithEvidence(ctx, report, false)
}

func applyEnvironmentChangesKnownPendingContext(ctx context.Context, report func(string)) (environmentApplyResult, error) {
	return applyEnvironmentChangesWithEvidence(ctx, report, true)
}

func applyEnvironmentChangesWithEvidence(
	ctx context.Context,
	report func(string),
	knownPending bool,
) (environmentApplyResult, error) {
	result := emptyEnvironmentApplyResult()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	previousPID, alive := daemon.IsDaemonRunning()
	result.DaemonRunning = alive
	result.PreviousPID = previousPID
	if !alive {
		return result, nil
	}

	client := daemon.NewClient(daemon.DefaultSocketPath()).WithContext(ctx)
	status, err := client.Status()
	if err != nil {
		return result, fmt.Errorf("checking environment changes: %w", err)
	}
	result.ChangesPending = status.ConfigStale || knownPending
	if !result.ChangesPending {
		result.PID = previousPID
		result.FinalStatus = status
		return result, nil
	}

	if _, err := config.Load(configFile); err != nil {
		return result, fmt.Errorf("cannot apply environment changes: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.reconcileDispatched = true
	reconciled, err := client.ReconcileEnvironment()
	if err != nil {
		return result, fmt.Errorf("reconciling environment changes: %w", err)
	}
	if !reconciled.RestartRequired {
		result.Applied = true
		result.PID = previousPID
		result.PreviouslyRunning = append(result.PreviouslyRunning, reconciled.PreviouslyRunning...)
		result.UnavailableResources = append(result.UnavailableResources, reconciled.UnavailableResources...)
		result.RestoredResources = availablePreviouslyRunning(
			reconciled.PreviouslyRunning,
			reconciled.UnavailableResources,
		)
		result.StartedDependencies = append(result.StartedDependencies, reconciled.StartedDependencies...)
		if len(reconciled.AffectedResources) == 0 {
			result.FinalStatus, err = client.Status()
		} else {
			result.FinalStatus, err = waitForLifecycleJSONContext(ctx, client, reconciled.AffectedResources, "healthy")
		}
		if err != nil {
			return result, fmt.Errorf("environment applied, but affected resources could not recover: %w", err)
		}
		return result, nil
	}

	result.PreviouslyRunning = runningEnvironmentResources(status.Resources)
	if report != nil {
		if len(result.PreviouslyRunning) == 0 {
			report("Applying environment updates...")
		} else {
			report(fmt.Sprintf(
				"Applying environment updates; %d running resource(s) will be restored...",
				len(result.PreviouslyRunning),
			))
		}
	}
	if _, err := client.DownAndWait(); err != nil {
		return result, fmt.Errorf("preparing running resources for environment update: %w", err)
	}
	_, pid, running, err := restartDaemonOperationContext(ctx, configFile, previousPID, true)
	if err != nil {
		return result, fmt.Errorf("applying environment changes: %w", err)
	}
	result.DaemonRunning = running
	result.PID = pid
	result.Applied = true

	client = daemon.NewClient(daemon.DefaultSocketPath()).WithContext(ctx)
	freshStatus, err := client.Status()
	if err != nil {
		return result, fmt.Errorf("checking applied environment: %w", err)
	}
	result.RestoredResources, result.UnavailableResources = restorableEnvironmentResources(
		result.PreviouslyRunning,
		freshStatus.Resources,
	)
	if len(result.RestoredResources) == 0 {
		result.FinalStatus = freshStatus
		return result, nil
	}

	if report != nil {
		report(fmt.Sprintf("Restoring %d running resource(s)...", len(result.RestoredResources)))
	}
	requestedRestores := append([]string(nil), result.RestoredResources...)
	response, err := client.Up(daemon.UpRequest{Resources: requestedRestores})
	if err != nil {
		return result, fmt.Errorf("restoring running resources: %w", err)
	}
	result.StartedDependencies = daemonsrv.AdditionalResourceNames(requestedRestores, response.AffectedResources)
	sort.Strings(result.RestoredResources)
	finalStatus, err := waitForLifecycleJSONContext(ctx, client, response.AffectedResources, "healthy")
	result.FinalStatus = finalStatus
	if err != nil {
		return result, fmt.Errorf("environment applied, but running resources could not be restored: %w", err)
	}
	return result, nil
}

func availablePreviouslyRunning(previouslyRunning, unavailable []string) []string {
	unavailableSet := make(map[string]bool, len(unavailable))
	for _, name := range unavailable {
		unavailableSet[name] = true
	}
	available := make([]string, 0, len(previouslyRunning))
	for _, name := range previouslyRunning {
		if !unavailableSet[name] {
			available = append(available, name)
		}
	}
	return available
}

func environmentApplyProgress() func(string) {
	if cli.JSONOutput {
		return nil
	}
	return func(message string) {
		fmt.Println(message)
	}
}

func emptyEnvironmentApplyResult() environmentApplyResult {
	return environmentApplyResult{
		PreviouslyRunning:    []string{},
		RestoredResources:    []string{},
		StartedDependencies:  []string{},
		UnavailableResources: []string{},
	}
}

func runningEnvironmentResources(resources []daemon.ResourceStatus) []string {
	names := make([]string, 0, len(resources))
	for i := range resources {
		switch resources[i].State {
		case "stopped", "stopping":
			continue
		default:
			names = append(names, resources[i].Name)
		}
	}
	sort.Strings(names)
	return names
}

func restorableEnvironmentResources(previouslyRunning []string, available []daemon.ResourceStatus) ([]string, []string) {
	exists := make(map[string]bool, len(available))
	for i := range available {
		exists[available[i].Name] = true
	}
	restored := make([]string, 0, len(previouslyRunning))
	unavailable := make([]string, 0)
	for _, name := range previouslyRunning {
		if exists[name] {
			restored = append(restored, name)
		} else {
			unavailable = append(unavailable, name)
		}
	}
	return restored, unavailable
}

func buildEnvironmentApplyJSONData(result environmentApplyResult) environmentApplyJSONData {
	resources := []daemon.ResourceStatus{}
	if result.FinalStatus != nil && result.FinalStatus.Resources != nil {
		resources = result.FinalStatus.Resources
	}
	return environmentApplyJSONData{
		Operation:            "env_apply",
		Applied:              result.Applied,
		DaemonRunning:        result.DaemonRunning,
		ChangesPending:       result.ChangesPending && !result.Applied,
		PreviousPID:          result.PreviousPID,
		PID:                  result.PID,
		PreviouslyRunning:    result.PreviouslyRunning,
		RestoredResources:    result.RestoredResources,
		StartedDependencies:  result.StartedDependencies,
		UnavailableResources: result.UnavailableResources,
		Resources:            resources,
	}
}

func environmentApplyRecommendedActions(result environmentApplyResult) []cli.JSONAction {
	if !result.DaemonRunning {
		return []cli.JSONAction{{
			Command:     "orbit up --json",
			Reason:      "Start the environment with the latest configuration.",
			Destructive: false,
		}}
	}
	return nil
}

func printEnvironmentApplyResult(result environmentApplyResult) {
	switch {
	case !result.DaemonRunning:
		fmt.Println("No running environment. Updates will be used on the next orbit up.")
	case !result.ChangesPending:
		fmt.Println("Environment is already up to date.")
	case len(result.RestoredResources) == 0:
		fmt.Println("Environment updates applied.")
	default:
		fmt.Printf("Environment updates applied. Restored %d running resource(s)", len(result.RestoredResources))
		if len(result.StartedDependencies) > 0 {
			noun := "dependencies"
			if len(result.StartedDependencies) == 1 {
				noun = "dependency"
			}
			fmt.Printf(
				"; started %d newly required %s",
				len(result.StartedDependencies),
				noun,
			)
		}
		fmt.Println(".")
	}
	if len(result.UnavailableResources) > 0 {
		fmt.Printf(
			"  %s no longer configured: %s\n",
			cli.Yellow.Sprint("!"),
			strings.Join(result.UnavailableResources, ", "),
		)
	}
}
