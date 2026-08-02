package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/spf13/cobra"
)

type uninstallOptions struct {
	yes   bool
	purge bool
}

type uninstallData struct {
	Operation          string   `json:"operation"`
	Removed            bool     `json:"removed"`
	Scheduled          bool     `json:"scheduled,omitempty"`
	Binary             string   `json:"binary"`
	Artifacts          []string `json:"artifacts"`
	UserData           string   `json:"user_data"`
	UserDataPreserved  bool     `json:"user_data_preserved"`
	DockerPreserved    bool     `json:"docker_preserved"`
	WorkspacePreserved bool     `json:"workspace_preserved"`
}

func uninstallCmd() *cobra.Command {
	var opts uninstallOptions
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Safely remove Orbit while preserving projects and Docker images",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUninstall(opts)
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "confirm the displayed uninstall plan without prompting")
	cmd.Flags().BoolVar(&opts.purge, "purge", false, "also remove Orbit settings, environments, logs, and state")
	return cmd
}

func runUninstall(opts uninstallOptions) error {
	binary, err := currentBinaryPath()
	if err != nil {
		return fmt.Errorf("resolve current binary: %w", err)
	}
	orbitHome := daemonsrv.OrbitDir()
	if opts.purge {
		if err := validatePurgeTarget(orbitHome); err != nil {
			return err
		}
	}
	artifacts := uninstallArtifacts(binary, orbitHome, opts.purge)
	data := uninstallData{
		Operation:          "uninstall",
		Binary:             binary,
		Artifacts:          artifacts,
		UserData:           orbitHome,
		UserDataPreserved:  !opts.purge,
		DockerPreserved:    true,
		WorkspacePreserved: true,
	}

	if !opts.yes {
		if cli.JSONOutput {
			return cli.WriteJSONSuccess(os.Stdout, commandString(), data, nil)
		}
		printUninstallPlan(os.Stdout, data)
		return nil
	}

	if pid, running := daemon.IsDaemonRunning(); running {
		client := daemon.NewClient(daemon.DefaultSocketPath())
		if _, err := client.DownAndWait(); err != nil {
			return fmt.Errorf("stop services and containers before uninstalling: %w", err)
		}
		if _, err := stopDaemon(pid); err != nil {
			return fmt.Errorf("stop daemon before uninstalling: %w", err)
		}
	}

	scheduled, err := removeUninstallArtifacts(artifacts)
	if err != nil {
		return err
	}
	data.Removed = !scheduled
	data.Scheduled = scheduled
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), data, nil)
	}
	printUninstallResult(os.Stdout, data)
	return nil
}

func uninstallArtifacts(binary, orbitHome string, purge bool) []string {
	artifacts := []string{binary}
	for _, path := range []string{binary + ".prev", binary + ".prev.failed"} {
		if _, err := os.Lstat(path); err == nil {
			artifacts = append(artifacts, path)
		}
	}
	if purge && pathExists(orbitHome) {
		artifacts = append(artifacts, orbitHome)
	}
	return artifacts
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func validatePurgeTarget(path string) error {
	if path == "" {
		return fmt.Errorf("refusing empty Orbit data purge target")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve Orbit data purge target: %w", err)
	}
	clean := filepath.Clean(abs)
	volume := filepath.VolumeName(clean)
	if clean == string(filepath.Separator) || clean == volume+string(filepath.Separator) {
		return fmt.Errorf("refusing unsafe Orbit data purge target: %s", path)
	}
	home, err := os.UserHomeDir()
	if err == nil && clean == filepath.Clean(home) {
		return fmt.Errorf("refusing to purge the user home directory: %s", path)
	}
	return nil
}

func printUninstallPlan(w io.Writer, data uninstallData) {
	fmt.Fprintln(w, "This will remove:")
	for _, artifact := range data.Artifacts {
		if artifact == data.UserData && !data.UserDataPreserved {
			fmt.Fprintf(w, "  user data: %s\n", artifact)
			continue
		}
		fmt.Fprintf(w, "  binary artifact: %s\n", artifact)
	}
	fmt.Fprintln(w, "This will preserve:")
	if data.UserDataPreserved {
		fmt.Fprintf(w, "  user data: %s\n", data.UserData)
	}
	fmt.Fprintln(w, "  Docker images and project workspaces")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Preview only. Run 'orbit uninstall --yes' to apply.")
}

func printUninstallResult(w io.Writer, data uninstallData) {
	if data.Scheduled {
		fmt.Fprintln(w, "Orbit will finish removing its binary after this command exits.")
	} else {
		fmt.Fprintln(w, "Orbit uninstalled.")
	}
	if data.UserDataPreserved {
		fmt.Fprintf(w, "User data preserved at %s.\n", data.UserData)
	} else {
		fmt.Fprintf(w, "User data removed from %s.\n", data.UserData)
	}
	fmt.Fprintln(w, "Docker images and project workspaces were preserved.")
}
