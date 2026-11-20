package app

import (
	"os"
	"testing"

	"github.com/iml885203/orbit/internal/cli"
)

func executeAppCommand(t *testing.T, args ...string) ([]byte, string, error) {
	t.Helper()
	oldArgs, oldConfig, oldGiven, oldKind, oldInstance := os.Args, configFile, configFlagGiven, configContextKind, instanceName
	oldGroups, oldInfra, oldTimeout := groups, infraOnly, timeout
	oldJSON, oldStart := cli.JSONOutput, cli.StartCommand
	oldID, oldAt, oldCommand := cliHistID, cliHistAt, cliHistCmd
	defer func() {
		os.Args, configFile, configFlagGiven, configContextKind, instanceName = oldArgs, oldConfig, oldGiven, oldKind, oldInstance
		groups, infraOnly, timeout = oldGroups, oldInfra, oldTimeout
		cli.JSONOutput, cli.StartCommand = oldJSON, oldStart
		cliHistID, cliHistAt, cliHistCmd = oldID, oldAt, oldCommand
	}()
	os.Args = append([]string{"orbit"}, args...)
	restore := captureLifecycleProcessStreams(t)
	defer restore()
	root := newRootCommand()
	root.SetArgs(args)
	err := root.ExecuteContext(t.Context())
	closeLifecycleCommand(root)
	if err != nil {
		err = contextualizeDaemonUnavailable(err)
		printExecutionError(os.Stderr, err)
	}
	out, diagnostics := restore()
	return out, diagnostics, err
}
