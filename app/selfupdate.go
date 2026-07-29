package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/spf13/cobra"
)

type selfUpdateJSONData struct {
	Operation                     string   `json:"operation"`
	BinaryPath                    string   `json:"binary_path"`
	PreviousBinaryPath            string   `json:"previous_binary_path"`
	RunningEnvironmentReconnected bool     `json:"running_environment_reconnected"`
	PreviouslyRunning             []string `json:"previously_running"`
	RestoredResources             []string `json:"restored_resources"`
}

// resolveInstallURL picks ORBIT_INSTALL_URL env > the distribution
// default ("" in an unbranded build — update then refuses with a
// configuration hint).
func resolveInstallURL() string {
	if url := os.Getenv("ORBIT_INSTALL_URL"); url != "" {
		return url
	}
	return distribution.InstallURL
}

func selfUpdateCmd() *cobra.Command {
	var rollback bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Download and install the latest orbit binary",
		Long: "Download and install the latest Orbit binary. If an environment is running, " +
			"Orbit reconnects it and restores the resources that were running.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if rollback {
				return runRollback()
			}
			return runSelfUpdate(cmd.Context())
		},
	}
	cmd.Flags().BoolVar(&rollback, "rollback", false, "Restore the previous binary from <path>.prev")
	return cmd
}

func runSelfUpdate(ctx context.Context) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf(
			"Windows Beta limitation: automatic update is not supported yet; " +
				"re-run: irm https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.ps1 | iex",
		)
	}
	installURL := resolveInstallURL()
	if installURL == "" {
		return fmt.Errorf("this build has no install URL configured; set ORBIT_INSTALL_URL to your install script")
	}
	exe, err := currentBinaryPath()
	if err != nil {
		return fmt.Errorf("resolve current binary: %w", err)
	}
	activeConfig, daemonWasRunning, err := updateHandoffContext()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Fetching %s\n", installURL)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	script, err := os.CreateTemp("", "orbit-install-*.sh")
	if err != nil {
		return fmt.Errorf("create temporary install script: %w", err)
	}
	scriptPath := script.Name()
	if err := script.Close(); err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("close temporary install script: %w", err)
	}
	defer os.Remove(scriptPath)

	curl := exec.CommandContext(ctx, "curl", "-fsSL", installURL, "-o", scriptPath)
	curl.Stdout = os.Stdout
	curl.Stderr = os.Stderr
	if err := curl.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("update timed out after 5m: %w", ctx.Err())
		}
		return fmt.Errorf("download install script: %w", err)
	}

	sh := exec.CommandContext(ctx, "bash", scriptPath)
	sh.Env = environmentWithValue(os.Environ(), "ORBIT_INSTALL_DIR", filepath.Dir(exe))
	sh.Stdout = os.Stdout
	if cli.JSONOutput {
		sh.Stdout = os.Stderr
	}
	sh.Stderr = os.Stderr
	if err := sh.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("update timed out after 5m: %w", ctx.Err())
		}
		return fmt.Errorf("run install script: %w", err)
	}

	restartResult := emptyDaemonRestartResult()
	if daemonWasRunning {
		var restartErr error
		restartResult, restartErr = restartDaemonPreservingResources(activeConfig, daemonRestartProgress())
		if restartErr != nil {
			return fmt.Errorf("Orbit was updated, but the running environment could not be restored: %w", restartErr)
		}
		fmt.Fprintf(os.Stderr, "Running environment restored (%d resource(s)).\n", len(restartResult.RestoredResources))
	}

	prev := exe + ".prev"
	if _, err := os.Stat(prev); err == nil {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "To roll back:   orbit update --rollback")
	}
	if cli.JSONOutput {
		return writeSelfUpdateJSON("update", exe, prev, restartResult)
	}
	return nil
}

func runRollback() error {
	exe, err := currentBinaryPath()
	if err != nil {
		return fmt.Errorf("resolve current binary: %w", err)
	}
	prev := exe + ".prev"
	if _, err := os.Stat(prev); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no previous version to roll back to (expected %s)", prev)
		}
		return fmt.Errorf("stat %s: %w", prev, err)
	}
	activeConfig, daemonWasRunning, err := updateHandoffContext()
	if err != nil {
		return err
	}

	failed := exe + ".prev.failed"
	if _, err := os.Stat(failed); err == nil {
		return fmt.Errorf("stale rollback artifact at %s — inspect and remove before retrying", failed)
	}
	if err := os.Rename(exe, failed); err != nil {
		return fmt.Errorf("move current binary aside (%s → %s): %w", exe, failed, err)
	}
	if err := os.Rename(prev, exe); err != nil {
		if restoreErr := os.Rename(failed, exe); restoreErr != nil {
			return fmt.Errorf("restore previous failed: %w (and could not restore current: %w)", err, restoreErr)
		}
		return fmt.Errorf("restore previous binary: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Rolled back: %s → %s\n", prev, exe)
	fmt.Fprintf(os.Stderr, "Previous current binary kept at %s (delete manually once you're sure).\n", failed)
	restartResult := emptyDaemonRestartResult()
	if daemonWasRunning {
		var restartErr error
		restartResult, restartErr = restartDaemonPreservingResources(activeConfig, daemonRestartProgress())
		if restartErr != nil {
			return fmt.Errorf("Orbit was rolled back, but the running environment could not be restored: %w", restartErr)
		}
		fmt.Fprintf(os.Stderr, "Running environment restored (%d resource(s)).\n", len(restartResult.RestoredResources))
	}
	if cli.JSONOutput {
		return writeSelfUpdateJSON("rollback", exe, failed, restartResult)
	}
	return nil
}

func emptyDaemonRestartResult() daemonRestartResult {
	return daemonRestartResult{
		PreviouslyRunning: []string{},
		RestoredResources: []string{},
	}
}

func writeSelfUpdateJSON(operation, binaryPath, previousBinaryPath string, result daemonRestartResult) error {
	return cli.WriteJSONSuccess(os.Stdout, commandString(), selfUpdateJSONData{
		Operation:                     operation,
		BinaryPath:                    binaryPath,
		PreviousBinaryPath:            previousBinaryPath,
		RunningEnvironmentReconnected: result.WasRunning && result.Running,
		PreviouslyRunning:             result.PreviouslyRunning,
		RestoredResources:             result.RestoredResources,
	}, nil)
}

func updateHandoffContext() (string, bool, error) {
	_, alive := daemon.IsDaemonRunning()
	if !alive {
		return "", false, nil
	}
	status, err := daemon.NewClient(daemon.DefaultSocketPath()).Status()
	if err != nil {
		return "", true, fmt.Errorf("checking the running environment before changing Orbit: %w", err)
	}
	if status.ConfigPath != "" {
		return status.ConfigPath, true, nil
	}
	return configFile, true, nil
}

func environmentWithValue(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		result = append(result, item)
	}
	return append(result, prefix+value)
}

func currentBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
