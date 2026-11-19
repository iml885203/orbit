package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsource"
	"github.com/spf13/cobra"
)

type environmentApplyPreviewJSON struct {
	Operation string `json:"operation"`
	daemon.EnvironmentApplyPreview
}

func environmentApplyPreviewCommand(cmd *cobra.Command) bool {
	if cmd == nil || cmd.Name() != "apply" || cmd.Parent() == nil || cmd.Parent().Name() != "env" {
		return false
	}
	dry, _ := cmd.Flags().GetBool("dry-run")
	return dry
}

func runEnvironmentApplyPreview(cmd *cobra.Command) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), effectiveTimeout(timeout))
	defer cancel()
	preview, err := previewEnvironmentChanges(ctx)
	data := environmentApplyPreviewJSON{Operation: "env_apply_preview", EnvironmentApplyPreview: preview}
	if cli.JSONOutput {
		if err != nil {
			if writeErr := cli.WriteJSONFailure(os.Stdout, commandString(), data, err, nil); writeErr != nil {
				return writeErr
			}
			return errCLIJSONAlreadyRendered{err: err}
		}
		return cli.WriteJSONSuccess(os.Stdout, commandString(), data, nil)
	}
	if err != nil {
		return err
	}
	if !preview.DaemonRunning {
		fmt.Println("No running environment. No resources would be interrupted.")
		return nil
	}
	switch {
	case !preview.ChangesPending:
		fmt.Println("Environment is already up to date.")
	case preview.RestartRequired:
		fmt.Println("A daemon restart is required; running resources would stop and surviving resources would be restored.")
	default:
		fmt.Println("Environment apply impact:")
	}
	for _, row := range []struct {
		label string
		names []string
	}{{"Stop", preview.StopResources}, {"Restart / restore", preview.RestartResources}, {"Removed from configuration", preview.RemovedResources}, {"Start required dependencies", preview.StartedDependencies}} {
		if len(row.names) > 0 {
			fmt.Printf("  %s: %s\n", row.label, strings.Join(row.names, ", "))
		}
	}
	fmt.Printf("Snapshot at %s, daemon PID %d. Apply revalidates and recalculates the impact.\n", preview.ObservedAt.Format(time.RFC3339), preview.PID)
	return nil
}

func previewEnvironmentChanges(ctx context.Context) (daemon.EnvironmentApplyPreview, error) {
	result := daemon.EnvironmentApplyPreview{ObservedAt: time.Now().UTC(), StopResources: []string{}, RestartResources: []string{}, RemovedResources: []string{}, StartedDependencies: []string{}, UnavailableResources: []string{}, AffectedResources: []string{}}
	if pid := daemon.ReadPID(); pid == 0 || !daemon.IsProcessAlive(pid) {
		if _, err := config.Load(configFile); err != nil {
			return result, fmt.Errorf("cannot preview environment changes: %w", err)
		}
		result.Context = daemon.EnvironmentContext{Kind: configContextKind, ConfigPath: configFile, Available: true}
		return result, nil
	}
	preview, err := daemon.NewClient(daemon.DefaultSocketPath()).WithContext(ctx).PreviewEnvironment(configFile)
	if err != nil {
		return result, err
	}
	return *preview, nil
}

func prepareEnvironmentApplyPreviewContext(cmd *cobra.Command) error {
	configFlagGiven = cmd.Root().PersistentFlags().Changed("config")
	cli.StartCommand = startCommand
	if configFile == "" {
		configFile = resolveConfigFile()
	}
	configContextKind = inferredEnvironmentContextKind(configFile)
	if configFlagGiven {
		configContextKind = "explicit"
	}
	registry, err := envsource.Load(envsource.RegistryPath(daemon.OrbitDir()))
	if err != nil {
		return err
	}
	if source, _, found := registry.SourceForPath(daemon.OrbitDir(), configFile); found {
		_ = os.Setenv("ORBIT_SOURCE_NAME", source.Name)
		if source.Workspace == "" {
			_ = os.Unsetenv("WORKSPACE_ROOT")
		} else {
			_ = os.Setenv("WORKSPACE_ROOT", source.Workspace)
		}
	} else {
		_ = os.Unsetenv("ORBIT_SOURCE_NAME")
	}
	return nil
}
