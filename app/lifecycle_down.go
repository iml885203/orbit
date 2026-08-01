package app

import (
	"fmt"
	"os"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

func reconcilableResourcesExist(configPath string) (bool, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return false, err
	}
	return len(persistedRuntimeStatus(configPath, cfg)) > 0, nil
}

func anyResourceActive(status *daemon.StatusResponse) bool {
	if status == nil {
		return false
	}
	for i := range status.Resources {
		if status.Resources[i].State != "stopped" {
			return true
		}
	}
	return false
}

func writeDownShortCircuit(message string, actions []cli.JSONAction, status *daemon.StatusResponse) error {
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildLifecycleJSONData(lifecycleJSONOptions{
			Operation:   "down",
			Message:     message,
			FinalStatus: status,
		}), actions)
	}
	fmt.Println(message)
	if message == downBeforeSetupMessage {
		_, _ = cli.Faint.Println("  Next: orbit init")
	}
	return nil
}
