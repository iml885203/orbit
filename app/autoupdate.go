package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/iml885203/orbit/autoupdate"
	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/instance"
	"github.com/iml885203/orbit/platform"
	"github.com/spf13/cobra"
)

const backgroundUpdateEnv = "ORBIT_UPDATE_BACKGROUND"

func automaticUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__update-check",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			launchPath, err := autoupdate.LaunchPath()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			checker := automaticUpdateChecker()
			state, err := checker.CheckAndStage(ctx, launchPath, buildVersion())
			if err != nil {
				return err
			}
			if state.Phase == "ready" {
				state = recordAutomaticApplyEligibility(state)
				if state.ApplyEligible {
					return launchUpdateWorker(state, os.Getpid(), false, true)
				}
			}
			return nil
		},
	}
}

func automaticUpdateWorkerCmd() *cobra.Command {
	var launchPath, stagedPath, installationID, operation, transactionID string
	var parentPID int
	cmd := &cobra.Command{
		Use:    "__update-apply",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUpdateWorker(operation, launchPath, stagedPath, installationID, transactionID, parentPID)
		},
	}
	cmd.Flags().StringVar(&launchPath, "launch-path", "", "installation launch path")
	cmd.Flags().StringVar(&stagedPath, "staged", "", "verified staged binary")
	cmd.Flags().StringVar(&installationID, "installation", "", "stable installation ID")
	cmd.Flags().StringVar(&operation, "operation", "update", "update or rollback")
	cmd.Flags().StringVar(&transactionID, "transaction", "", "update transaction ID")
	cmd.Flags().IntVar(&parentPID, "parent", 0, "process that must exit before replacement")
	_ = cmd.MarkFlagRequired("launch-path")
	_ = cmd.MarkFlagRequired("installation")
	_ = cmd.MarkFlagRequired("transaction")
	return cmd
}

func automaticUpdateChecker() autoupdate.Checker {
	endpoint := distribution.ReleaseAPIURL
	if override := strings.TrimSpace(os.Getenv("ORBIT_RELEASE_API_URL")); override != "" {
		endpoint = override
	}
	return autoupdate.Checker{Channel: autoupdate.Channel{
		ReleaseAPIURL: endpoint, ReleaseRepository: distribution.ReleaseRepository,
	}}
}

func invokedBinaryPath() (string, error) {
	value := os.Args[0]
	if !strings.ContainsRune(value, os.PathSeparator) {
		resolved, err := exec.LookPath(value)
		if err != nil {
			return "", fmt.Errorf("resolve invoked Orbit binary: %w", err)
		}
		value = resolved
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve invoked Orbit path: %w", err)
	}
	return abs, nil
}

func showAutomaticUpdateDisclosure(cmd *cobra.Command) {
	if distribution.ReleaseAPIURL == "" || cli.JSONOutput || os.Getenv(backgroundUpdateEnv) == "1" || cmd.Name() == "__update-check" || cmd.Name() == "version" {
		return
	}
	launchPath, err := invokedBinaryPath()
	if err != nil {
		return
	}
	state, err := autoupdate.Load(launchPath)
	if err != nil || state.DisclosureShown {
		return
	}
	if state.Owner == autoupdate.OwnerDirect {
		_, _ = fmt.Fprintln(os.Stderr, "Orbit checks for verified updates in the background and applies them after all product resources stop.")
	} else {
		_, _ = fmt.Fprintf(os.Stderr, "Orbit checks for updates; %s remains responsible for installing them.\n", updateOwnerName(state.Owner))
	}
	_, _ = fmt.Fprintln(os.Stderr, "Automatic updates: On    Change with `orbit settings set automatic-updates off`.")
	_, _ = autoupdate.Update(launchPath, func(next *autoupdate.State) error {
		next.DisclosureShown = true
		return nil
	})
}

func configureAutomaticUpdateInstallation() {
	if strings.TrimSpace(os.Getenv(autoupdate.EnvLaunchPath)) != "" {
		return
	}
	if launchPath, err := invokedBinaryPath(); err == nil {
		_ = os.Setenv(autoupdate.EnvLaunchPath, launchPath)
	}
}

func waitForPendingAutomaticUpdate(cmd *cobra.Command) error {
	if !updateMutationCommand(cmd) || cmd.Name() == "__update-apply" {
		return nil
	}
	launchPath, err := invokedBinaryPath()
	if err != nil {
		return nil
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		state, loadErr := autoupdate.Load(launchPath)
		if loadErr != nil {
			return loadErr
		}
		if state.Transaction == nil || state.Transaction.FinishedAt != nil {
			return nil
		}
		if os.Getenv(daemon.UpdateTransactionEnv) == state.Transaction.ID {
			return nil
		}
		lastProgress := state.Transaction.StartedAt
		if state.Transaction.HeartbeatAt != nil {
			lastProgress = *state.Transaction.HeartbeatAt
		}
		if time.Since(lastProgress) > 30*time.Second {
			staleErr := fmt.Errorf("update worker stopped during %s", state.Transaction.Phase)
			_, _ = autoupdate.FinishTransaction(launchPath, state.Transaction.ID, "failed", staleErr)
			return updateRecoveryRequiredError{cause: staleErr.Error()}
		}
		if time.Now().After(deadline) {
			return updateInProgressError{phase: state.Transaction.Phase}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type updateRecoveryRequiredError struct{ cause string }

func (e updateRecoveryRequiredError) Error() string     { return e.cause }
func (e updateRecoveryRequiredError) ErrorCode() string { return "update_recovery_required" }
func (e updateRecoveryRequiredError) CLIJSONHint() string {
	return "The durable update journal was marked failed; retry the verified update before mutating resources."
}
func (e updateRecoveryRequiredError) CLIJSONReplacementActions() []cli.JSONAction {
	return []cli.JSONAction{{Command: "orbit update --json", Reason: "Retry the verified update transaction."}}
}

type updateInProgressError struct{ phase string }

func (e updateInProgressError) Error() string {
	return fmt.Sprintf("Orbit is finishing a verified update (%s)", e.phase)
}

func (e updateInProgressError) ErrorCode() string { return "update_in_progress" }

func (e updateInProgressError) CLIJSONHint() string {
	return "Read-only status remains available while the update finishes."
}

func (e updateInProgressError) CLIJSONReplacementActions() []cli.JSONAction {
	return []cli.JSONAction{{
		Command: "orbit status --json", Reason: "Observe the durable update transaction before retrying the mutation.",
	}}
}

func updateMutationCommand(cmd *cobra.Command) bool {
	top := cmd
	for top.Parent() != nil && top.Parent().Parent() != nil {
		top = top.Parent()
	}
	switch top.Name() {
	case "status", "inspect", "logs", "doctor", "version", "history", "open", "help", "completion":
		return false
	case "settings":
		return cmd.Name() != "list"
	case "instance":
		return cmd.Name() != "list"
	case "daemon":
		return cmd.Name() != "status"
	default:
		return true
	}
}

func scheduleAutomaticUpdateCheck() {
	if os.Getenv(backgroundUpdateEnv) == "1" || distribution.ReleaseAPIURL == "" {
		return
	}
	launchPath, err := invokedBinaryPath()
	if err != nil {
		return
	}
	if state, stateErr := autoupdate.Load(launchPath); stateErr == nil &&
		state.Policy == autoupdate.PolicyAutomatic && state.Owner == autoupdate.OwnerDirect &&
		state.Phase == "ready" && state.StagedBinary != "" {
		state = recordAutomaticApplyEligibility(state)
		if state.ApplyEligible {
			_ = launchUpdateWorker(state, os.Getpid(), false, true)
			return
		}
	}
	claimed, err := automaticUpdateChecker().ClaimBackgroundCheck(launchPath)
	if err != nil || !claimed {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "__update-check")
	cmd.Env = environmentWithValue(os.Environ(), backgroundUpdateEnv, "1")
	cmd.Stdout = nil
	cmd.Stderr = nil
	platform.DetachProcess(cmd)
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

func automaticApplyEligible(state autoupdate.State) bool {
	if hasUnregisteredDiscoverableRuntime(state) {
		return false
	}
	for _, runtimeState := range state.Runtimes {
		if !runtimeRegistrationCurrent(runtimeState) {
			continue
		}
		status, err := daemon.NewClient(runtimeState.SocketPath).Status()
		if err != nil {
			return false
		}
		for _, resource := range status.Resources {
			if resource.State != "stopped" {
				return false
			}
		}
	}
	return true
}

func hasUnregisteredDiscoverableRuntime(state autoupdate.State) bool {
	registered := make(map[string]bool, len(state.Runtimes))
	for _, runtimeState := range state.Runtimes {
		if runtimeRegistrationCurrent(runtimeState) {
			registered[filepath.Clean(runtimeState.SocketPath)] = true
		}
	}
	for _, socketPath := range discoverableDefaultRuntimeSockets() {
		if daemon.NewClient(socketPath).Health() == nil && !registered[socketPath] {
			return true
		}
	}
	instances, err := instance.List(instance.BaseHome())
	if err != nil {
		return true
	}
	for _, summary := range instances {
		if summary.State == "running" && !registered[filepath.Clean(summary.SocketPath)] {
			return true
		}
	}
	return false
}

func discoverableDefaultRuntimeSockets() []string {
	current := filepath.Clean(daemon.DefaultSocketPath())
	base := filepath.Clean(filepath.Join(instance.BaseHome(), "orbit.sock"))
	if current == base {
		return []string{current}
	}
	return []string{base, current}
}

func recordAutomaticApplyEligibility(state autoupdate.State) autoupdate.State {
	eligible := automaticApplyEligible(state)
	updated, err := autoupdate.Update(state.LaunchPath, func(next *autoupdate.State) error {
		next.ApplyEligible = eligible
		next.DeferReason = ""
		if !eligible {
			next.DeferReason = "resources_running"
		}
		return nil
	})
	if err != nil {
		return state
	}
	return updated
}

func launchUpdateWorker(state autoupdate.State, parentPID int, wait, automatic bool) error {
	if state.StagedBinary == "" {
		return fmt.Errorf("orbit %s is not staged", state.TargetVersion)
	}
	operation := "update"
	if automatic {
		operation = "automatic"
	}
	helper, err := prepareUpdateHelper(state.InstallationID)
	if err != nil {
		return err
	}
	claimed, err := autoupdate.BeginTransaction(state.LaunchPath, operation, state.TargetVersion)
	if err != nil {
		_ = os.Remove(helper)
		return err
	}
	state = claimed
	args := []string{"__update-apply", "--operation", operation, "--launch-path", state.LaunchPath, "--staged", state.StagedBinary, "--installation", state.InstallationID, "--transaction", state.Transaction.ID}
	if parentPID > 0 {
		args = append(args, "--parent", fmt.Sprint(parentPID))
	}
	cmd := exec.Command(helper, args...)
	cmd.Env = environmentWithValue(os.Environ(), backgroundUpdateEnv, "1")
	if wait {
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			_ = os.Remove(helper)
			_, _ = autoupdate.FinishTransaction(state.LaunchPath, state.Transaction.ID, "failed", err)
			return err
		}
		_, _ = autoupdate.SetTransactionWorker(state.LaunchPath, state.Transaction.ID, cmd.Process.Pid)
		err := cmd.Wait()
		if err != nil {
			_, _ = autoupdate.FinishTransaction(state.LaunchPath, state.Transaction.ID, "failed", err)
		}
		return err
	}
	platform.DetachProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(helper)
		_, _ = autoupdate.FinishTransaction(state.LaunchPath, state.Transaction.ID, "failed", err)
		return fmt.Errorf("start update helper: %w", err)
	}
	_, _ = autoupdate.SetTransactionWorker(state.LaunchPath, state.Transaction.ID, cmd.Process.Pid)
	_ = cmd.Process.Release()
	return nil
}

func launchRollbackWorker(state autoupdate.State, parentPID int, wait bool) error {
	target := "previous"
	if version, probeErr := probeInstalledVersion(context.Background(), state.LaunchPath+".prev"); probeErr == nil {
		target = version
	}
	helper, err := prepareUpdateHelper(state.InstallationID)
	if err != nil {
		return err
	}
	claimed, err := autoupdate.BeginTransaction(state.LaunchPath, "rollback", target)
	if err != nil {
		_ = os.Remove(helper)
		return err
	}
	state = claimed
	args := []string{"__update-apply", "--operation", "rollback", "--launch-path", state.LaunchPath, "--installation", state.InstallationID, "--transaction", state.Transaction.ID}
	if parentPID > 0 {
		args = append(args, "--parent", fmt.Sprint(parentPID))
	}
	cmd := exec.Command(helper, args...)
	cmd.Env = environmentWithValue(os.Environ(), backgroundUpdateEnv, "1")
	if wait {
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			_ = os.Remove(helper)
			_, _ = autoupdate.FinishTransaction(state.LaunchPath, state.Transaction.ID, "failed", err)
			return err
		}
		_, _ = autoupdate.SetTransactionWorker(state.LaunchPath, state.Transaction.ID, cmd.Process.Pid)
		err := cmd.Wait()
		if err != nil {
			_, _ = autoupdate.FinishTransaction(state.LaunchPath, state.Transaction.ID, "failed", err)
		}
		return err
	}
	platform.DetachProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(helper)
		_, _ = autoupdate.FinishTransaction(state.LaunchPath, state.Transaction.ID, "failed", err)
		return err
	}
	_, _ = autoupdate.SetTransactionWorker(state.LaunchPath, state.Transaction.ID, cmd.Process.Pid)
	_ = cmd.Process.Release()
	return nil
}

func prepareUpdateHelper(installationID string) (string, error) {
	dir, err := autoupdate.GlobalDir()
	if err != nil {
		return "", err
	}
	helperDir := filepath.Join(dir, "helpers", installationID)
	if err := os.MkdirAll(helperDir, 0o700); err != nil {
		return "", fmt.Errorf("create update helper directory: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve update helper source: %w", err)
	}
	helper := filepath.Join(helperDir, fmt.Sprintf("orbit-helper-%d", time.Now().UnixNano()))
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	if err := copyUpdateHelper(exe, helper); err != nil {
		return "", err
	}
	return helper, nil
}

func runUpdateWorker(operation, launchPath, stagedPath, installationID, transactionID string, parentPID int) error {
	defer cleanUpdateHelper(installationID)
	if state, err := autoupdate.Load(launchPath); err != nil {
		return err
	} else if state.InstallationID != installationID {
		return fmt.Errorf("update installation identity changed")
	}
	for parentPID > 0 && platform.IsProcessAlive(parentPID) {
		time.Sleep(25 * time.Millisecond)
	}
	state, err := autoupdate.Load(launchPath)
	if err != nil {
		return err
	}
	if state.Transaction == nil || state.Transaction.FinishedAt != nil || state.Transaction.ID != transactionID || state.Transaction.Operation != operation {
		return fmt.Errorf("update helper does not own the active transaction")
	}
	var candidate *autoupdate.VerifiedCandidate
	if operation != "rollback" {
		candidate, err = openUpdateCandidate(state, stagedPath)
		if err != nil {
			_, _ = autoupdate.FinishTransaction(launchPath, transactionID, "failed", err)
			return err
		}
		defer func(opened *autoupdate.VerifiedCandidate) { _ = opened.Close() }(candidate)
	}
	if err := os.Setenv(daemon.UpdateTransactionEnv, transactionID); err != nil {
		return fmt.Errorf("configure update transaction context: %w", err)
	}
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go heartbeatUpdateTransaction(launchPath, state.Transaction.ID, heartbeatDone)
	snapshots, err := drainUpdateRuntimes(state, operation != "automatic")
	if err != nil {
		if errors.Is(err, errUpdateResourcesRunning) {
			_, _ = autoupdate.Update(launchPath, func(next *autoupdate.State) error {
				next.ApplyEligible = false
				next.DeferReason = "resources_running"
				return nil
			})
			_, _ = autoupdate.FinishTransaction(launchPath, transactionID, "ready", nil)
			return nil
		}
		_, _ = autoupdate.FinishTransaction(launchPath, transactionID, "failed", err)
		return err
	}
	var replaceErr error
	if operation == "rollback" {
		replaceErr = autoupdate.Restore(launchPath)
	} else {
		_, replaceErr = autoupdate.ReplaceCandidate(launchPath, candidate)
		_ = candidate.Close()
		candidate = nil
	}
	if replaceErr != nil {
		reconnectDrainedRuntimes(launchPath, snapshots)
		_, _ = autoupdate.FinishTransaction(launchPath, transactionID, "failed", replaceErr)
		return replaceErr
	}
	probeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	installedVersion, verifyErr := probeInstalledVersion(probeCtx, launchPath)
	if verifyErr == nil && !autoupdate.VersionsMatch(installedVersion, state.Transaction.TargetVersion) {
		verifyErr = fmt.Errorf("target reported %s, expected %s", installedVersion, state.Transaction.TargetVersion)
	}
	if verifyErr != nil {
		var restoreErr error
		if operation == "rollback" {
			restoreErr = autoupdate.UndoRestore(launchPath)
		} else {
			restoreErr = autoupdate.Restore(launchPath)
		}
		if restoreErr != nil {
			verifyErr = fmt.Errorf("target verification failed: %w; rollback failed: %v", verifyErr, restoreErr)
		}
		reconnectDrainedRuntimes(launchPath, snapshots)
		_, _ = autoupdate.FinishTransaction(launchPath, transactionID, "failed", verifyErr)
		return verifyErr
	}
	started := make(map[string]drainedRuntime)
	for identity, snapshot := range snapshots {
		outcome := startRegisteredRuntime(launchPath, snapshot)
		_, _ = autoupdate.RecordRuntimeOutcome(launchPath, transactionID, identity, outcome)
		if outcome.Error != "" {
			stopTargetRuntimes(started, state.Transaction.ID)
			var rollbackErr error
			if operation == "rollback" {
				rollbackErr = autoupdate.UndoRestore(launchPath)
			} else {
				rollbackErr = autoupdate.Restore(launchPath)
			}
			reconnectDrainedRuntimes(launchPath, snapshots)
			startErr := fmt.Errorf("target daemon failed to start for runtime %s: %s", identity, outcome.Error)
			if rollbackErr != nil {
				startErr = fmt.Errorf("%w; binary rollback failed: %v", startErr, rollbackErr)
			}
			_, _ = autoupdate.FinishTransaction(launchPath, transactionID, "failed", startErr)
			return startErr
		}
		started[identity] = snapshot
	}
	partial := false
	for identity, snapshot := range snapshots {
		outcome := restoreRuntimeResources(launchPath, snapshot)
		if outcome.Error != "" {
			partial = true
		}
		_, _ = autoupdate.RecordRuntimeOutcome(launchPath, transactionID, identity, outcome)
	}
	phase := "succeeded"
	if partial {
		phase = "partial"
	}
	_, err = autoupdate.FinishTransaction(launchPath, transactionID, phase, nil)
	if err == nil && operation != "rollback" {
		autoupdate.RemoveStagedBinary(stagedPath)
	}
	return err
}

func openUpdateCandidate(state autoupdate.State, stagedPath string) (*autoupdate.VerifiedCandidate, error) {
	if filepath.Clean(stagedPath) != filepath.Clean(state.StagedBinary) || state.StagedEvidence == nil {
		return nil, fmt.Errorf("staged Orbit has no matching verified release evidence")
	}
	assetName, err := autoupdate.PlatformAssetName()
	if err != nil {
		return nil, err
	}
	if err := state.StagedEvidence.ValidateForApply(
		state.Transaction.TargetVersion, distribution.ReleaseRepository, assetName); err != nil {
		return nil, err
	}
	return autoupdate.OpenVerifiedCandidate(stagedPath, state.StagedEvidence.AssetSHA256)
}

func heartbeatUpdateTransaction(launchPath, transactionID string, done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if autoupdate.HeartbeatTransaction(launchPath, transactionID) != nil {
				return
			}
		}
	}
}

func cleanUpdateHelper(installationID string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir, err := autoupdate.GlobalDir()
	if err != nil {
		return
	}
	helperDir := filepath.Join(dir, "helpers", installationID)
	relative, err := filepath.Rel(helperDir, exe)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return
	}
	if runtime.GOOS != "windows" {
		_ = os.Remove(exe)
		return
	}
	cleanup := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"Start-Sleep -Milliseconds 500; Remove-Item -LiteralPath $args[0] -Force", exe)
	platform.DetachProcess(cleanup)
	if cleanup.Start() == nil {
		_ = cleanup.Process.Release()
	}
}

func probeInstalledVersion(ctx context.Context, path string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return "", fmt.Errorf("orbit binary reported no version")
	}
	return fields[0], nil
}

type drainedRuntime struct {
	Registration      autoupdate.Runtime
	ConfigPath        string
	PreviouslyRunning []string
}

var errUpdateResourcesRunning = errors.New("product resources started before automatic update apply")

var drainUpdateRuntimes = drainRegisteredRuntimes

func drainRegisteredRuntimes(state autoupdate.State, allowRunning bool) (map[string]drainedRuntime, error) {
	if hasUnregisteredDiscoverableRuntime(state) {
		return nil, fmt.Errorf("a discoverable Orbit runtime has not registered for coordinated update; restart it with the installed Orbit build")
	}
	snapshots := make(map[string]drainedRuntime)
	for identity, runtimeState := range state.Runtimes {
		if !runtimeRegistrationCurrent(runtimeState) {
			continue
		}
		client := daemon.NewClient(runtimeState.SocketPath)
		transactionID := ""
		if state.Transaction != nil {
			transactionID = state.Transaction.ID
		}
		if _, err := client.DrainForUpdate(transactionID); err != nil {
			return snapshots, fmt.Errorf("drain admitted mutations for runtime %s: %w", identity, err)
		}
		status, err := client.Status()
		if err != nil {
			return snapshots, fmt.Errorf("snapshot runtime %s: %w", identity, err)
		}
		snapshot := drainedRuntime{Registration: runtimeState, ConfigPath: status.ConfigPath}
		snapshot.PreviouslyRunning = runningEnvironmentResources(status.Resources)
		if !allowRunning && len(snapshot.PreviouslyRunning) > 0 {
			return snapshots, errUpdateResourcesRunning
		}
		snapshots[identity] = snapshot
	}
	for identity, snapshot := range snapshots {
		client := daemon.NewClient(snapshot.Registration.SocketPath)
		transactionID := ""
		if state.Transaction != nil {
			transactionID = state.Transaction.ID
		}
		if _, err := client.DownForUpdate(true, transactionID); err != nil {
			return snapshots, fmt.Errorf("drain runtime %s: %w", identity, err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for platform.IsProcessAlive(snapshot.Registration.PID) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if platform.IsProcessAlive(snapshot.Registration.PID) {
			return snapshots, fmt.Errorf("runtime %s did not stop before update", identity)
		}
	}
	return snapshots, nil
}

func reconnectDrainedRuntimes(launchPath string, snapshots map[string]drainedRuntime) {
	for _, snapshot := range snapshots {
		_ = restoreRegisteredRuntime(launchPath, snapshot)
	}
}

func restoreRegisteredRuntime(launchPath string, snapshot drainedRuntime) autoupdate.RuntimeOutcome {
	outcome := startRegisteredRuntime(launchPath, snapshot)
	if outcome.Error != "" {
		return outcome
	}
	return restoreRuntimeResources(launchPath, snapshot)
}

func startRegisteredRuntime(launchPath string, snapshot drainedRuntime) autoupdate.RuntimeOutcome {
	outcome := autoupdate.RuntimeOutcome{
		Phase: "restored", PreviouslyRunning: snapshot.PreviouslyRunning,
		RestoredResources: []string{},
	}
	baseArgs := runtimeArgs(snapshot.Registration)
	startArgs := append(append([]string{}, baseArgs...), "--config", snapshot.ConfigPath, "daemon", "start", "--json")
	start := exec.Command(launchPath, startArgs...)
	start.Env = runtimeEnvironment(os.Environ(), snapshot.Registration)
	if output, err := start.CombinedOutput(); err != nil {
		outcome.Phase = "failed"
		outcome.Error = fmt.Sprintf("start target daemon: %v (%s)", err, strings.TrimSpace(string(output)))
		return outcome
	}
	return outcome
}

func restoreRuntimeResources(launchPath string, snapshot drainedRuntime) autoupdate.RuntimeOutcome {
	outcome := autoupdate.RuntimeOutcome{
		Phase: "restored", PreviouslyRunning: snapshot.PreviouslyRunning,
		RestoredResources: []string{},
	}
	if len(snapshot.PreviouslyRunning) == 0 {
		return outcome
	}
	baseArgs := runtimeArgs(snapshot.Registration)
	upArgs := append(append([]string{}, baseArgs...), "--config", snapshot.ConfigPath, "up")
	upArgs = append(upArgs, snapshot.PreviouslyRunning...)
	upArgs = append(upArgs, "--json")
	up := exec.Command(launchPath, upArgs...)
	up.Env = runtimeEnvironment(os.Environ(), snapshot.Registration)
	output, err := up.Output()
	if err != nil {
		outcome.Phase = "failed"
		outcome.Error = fmt.Sprintf("restore resources: %v", err)
		return outcome
	}
	var envelope struct {
		Data struct {
			AffectedResources []string `json:"affected_resources"`
		} `json:"data"`
	}
	if json.Unmarshal(output, &envelope) == nil {
		outcome.RestoredResources = envelope.Data.AffectedResources
	}
	return outcome
}

func stopTargetRuntimes(snapshots map[string]drainedRuntime, transactionID string) {
	for _, snapshot := range snapshots {
		client := daemon.NewClient(snapshot.Registration.SocketPath)
		_, _ = client.DownForUpdate(true, transactionID)
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := client.Status(); err != nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func runtimeArgs(runtimeState autoupdate.Runtime) []string {
	if runtimeState.Instance == "" {
		return nil
	}
	return []string{"--instance", runtimeState.Instance}
}

func runtimeEnvironment(environment []string, runtimeState autoupdate.Runtime) []string {
	filtered := environmentWithoutKeys(environment, "ORBIT_HOME", instance.EnvName, instance.EnvBaseHome, "ORBIT_NAMESPACE")
	if runtimeState.Instance == "" {
		return environmentWithValue(filtered, "ORBIT_HOME", runtimeState.Home)
	}
	base := filepath.Dir(filepath.Dir(runtimeState.Home))
	return environmentWithValue(filtered, instance.EnvBaseHome, base)
}

func environmentWithoutKeys(environment []string, keys ...string) []string {
	prefixes := make([]string, len(keys))
	for i, key := range keys {
		prefixes[i] = key + "="
	}
	result := make([]string, 0, len(environment))
	for _, item := range environment {
		keep := true
		for _, prefix := range prefixes {
			if strings.HasPrefix(item, prefix) {
				keep = false
				break
			}
		}
		if keep {
			result = append(result, item)
		}
	}
	return result
}

func copyUpdateHelper(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read update helper: %w", err)
	}
	if err := os.WriteFile(destination, data, 0o700); err != nil {
		return fmt.Errorf("write update helper: %w", err)
	}
	return nil
}

func registerUpdateRuntime() func() {
	launchPath, err := autoupdate.LaunchPath()
	if err != nil {
		return func() {}
	}
	home := daemon.OrbitDir()
	identity := "default"
	if name := instance.CurrentName(); name != "" {
		identity = "instance:" + name
	}
	started := time.Now().UTC()
	if info, statErr := os.Stat(daemon.DefaultPIDPath()); statErr == nil {
		started = info.ModTime().UTC()
	}
	_, _ = autoupdate.RegisterRuntime(launchPath, autoupdate.Runtime{
		Identity: identity, Home: home, SocketPath: daemon.DefaultSocketPath(),
		Instance: instance.CurrentName(), Executable: launchPath, Build: buildVersion(),
		PID: os.Getpid(), ProcessStarted: started.Format(time.RFC3339Nano),
	})
	return func() { _, _ = autoupdate.UnregisterRuntime(launchPath, identity) }
}

func runtimeRegistrationCurrent(runtimeState autoupdate.Runtime) bool {
	if runtimeState.PID <= 0 || !platform.IsProcessAlive(runtimeState.PID) {
		return false
	}
	pidPath := filepath.Join(runtimeState.Home, "orbit.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	var ownership struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(data, &ownership) != nil || ownership.PID != runtimeState.PID {
		return false
	}
	info, err := os.Stat(pidPath)
	if err != nil {
		return false
	}
	return info.ModTime().UTC().Format(time.RFC3339Nano) == runtimeState.ProcessStarted
}

func updateOwnerName(owner string) string {
	switch owner {
	case autoupdate.OwnerHomebrew:
		return "Homebrew"
	case autoupdate.OwnerScoop:
		return "Scoop"
	default:
		return "the installer"
	}
}
