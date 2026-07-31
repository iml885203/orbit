package app

import (
	"fmt"
	"os"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
)

// Deciding whether 'orbit down' has anything to stop is its own problem: the
// daemon may be gone while its containers and processes survive, so the answer
// comes from persisted state rather than a live snapshot.

// reconcilableResourcesExist reports whether persisted state still describes
// live resources for this config. With the daemon gone this is the only
// evidence that 'down' has work to do.
func reconcilableResourcesExist(configPath string) (bool, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return false, err
	}
	return len(persistedRuntimeStatus(configPath, cfg)) > 0, nil
}

// anyResourceActive reports whether stopping the environment would change
// anything. Any state other than "stopped" counts: a pending or degraded
// resource still owns a process or container that 'down' must clean up.
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

// writeDownShortCircuit reports a 'down' that had nothing to stop, in whichever
// output mode is active. status is nil when no daemon answered, so there is no
// resource snapshot to report.
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
