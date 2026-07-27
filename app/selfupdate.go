package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

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
	installURL := resolveInstallURL()
	if installURL == "" {
		return fmt.Errorf("this build has no install URL configured; set ORBIT_INSTALL_URL to your install script")
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
	sh.Stdout = os.Stdout
	sh.Stderr = os.Stderr
	if err := sh.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("update timed out after 5m: %w", ctx.Err())
		}
		return fmt.Errorf("run install script: %w", err)
	}

	if exe, err := currentBinaryPath(); err == nil {
		prev := exe + ".prev"
		if _, err := os.Stat(prev); err == nil {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "To roll back:   orbit update --rollback")
			fmt.Fprintf(os.Stderr, "Or manually:    mv %s %s && orbit daemon restart\n", prev, exe)
		}
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
	fmt.Fprintln(os.Stderr, "Run 'orbit daemon restart' to apply.")
	return nil
}

func currentBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
