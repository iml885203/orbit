package app

import (
	"os"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsource"
	"github.com/spf13/cobra"
)

func readOnlyEnvironmentCommand(cmd *cobra.Command) bool {
	return environmentApplyPreviewCommand(cmd) || (cmd != nil && cmd.Name() == "ready" && cmd.Parent() == cmd.Root())
}

func prepareReadOnlyEnvironmentContext(cmd *cobra.Command) error {
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
