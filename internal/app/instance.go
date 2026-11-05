package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/container"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/daemonsrv"
	"github.com/iml885203/orbit/internal/instance"
	"github.com/iml885203/orbit/internal/process"
	"github.com/spf13/cobra"
)

func instanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Discover and clean isolated runtime instances",
		Long: "Discover and clean isolated runtime instances. A named instance is created\n" +
			"when a lifecycle command first targets it with '--instance <name>'.",
	}
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List named runtime instances", Args: cobra.NoArgs, RunE: runInstanceList})
	cmd.AddCommand(&cobra.Command{
		Use:   "clean <name>",
		Short: "Remove a named runtime instance and its local data",
		Long: "Stop a named runtime instance, then permanently remove its daemon state and\n" +
			"owned Docker containers, networks, and volumes. Data stored in those volumes is lost.",
		Args: cobra.ExactArgs(1), RunE: runInstanceClean,
	})
	return cmd
}

func runInstanceList(_ *cobra.Command, _ []string) error {
	instances, err := instance.List(instance.BaseHome())
	if err != nil {
		return err
	}
	for i := range instances {
		describeInstanceResources(&instances[i])
	}
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), struct {
			Instances []instance.Summary `json:"instances"`
		}{Instances: instances}, nil)
	}
	if len(instances) == 0 {
		fmt.Println("No named instances.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tENVIRONMENT\tSTATE\tRESOURCES\tLAST ACTIVE\tENDPOINTS")
	for _, item := range instances {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", item.Name, item.Environment, item.State,
			formatResourceStates(item.ResourceStates), formatLastActive(item.LastActiveAt), formatInstanceEndpoints(item.Endpoints))
	}
	return w.Flush()
}

// describeInstanceResources fills endpoints and per-state resource counts
// from one Status call to the instance's daemon. Counts stay nil unless that
// call succeeds, so a stopped or unreachable daemon reads as unknown.
func describeInstanceResources(item *instance.Summary) {
	endpoints := make(map[string]string)
	if item.Dashboard != "" {
		endpoints["dashboard"] = item.Dashboard
	}
	item.Endpoints = endpoints
	if item.State != "running" {
		return
	}
	status, err := daemon.NewClient(item.SocketPath).Status()
	if err != nil {
		return
	}
	states := make(map[string]int)
	for _, resource := range status.Resources {
		states[resource.State]++
		if resource.URL != "" {
			endpoints[resource.Name] = resource.URL
			continue
		}
		labels := make([]string, 0, len(resource.Ports))
		for label := range resource.Ports {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		for _, label := range labels {
			key := resource.Name
			if len(labels) > 1 {
				key += "." + label
			}
			endpoints[key] = fmt.Sprintf("localhost:%d", resource.Ports[label])
		}
	}
	item.ResourceStates = states
}

func formatResourceStates(states map[string]int) string {
	if states == nil {
		return "-"
	}
	if len(states) == 0 {
		return "none"
	}
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%d %s", states[name], name))
	}
	return strings.Join(parts, ", ")
}

func formatLastActive(at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	return at.Local().Format("2006-01-02 15:04")
}

func formatInstanceEndpoints(endpoints map[string]string) string {
	keys := make([]string, 0, len(endpoints))
	for key := range endpoints {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+endpoints[key])
	}
	return strings.Join(parts, ", ")
}

func runInstanceClean(_ *cobra.Command, args []string) error {
	name := args[0]
	baseHome := instance.BaseHome()
	manifest, err := instance.ReadManifest(baseHome, name)
	if err != nil {
		if os.IsNotExist(err) {
			// No manifest but a directory still there: an earlier clean died
			// between emptying the home and removing it. Finish that rather
			// than reporting the instance missing — the caller asked for it
			// gone, and "does not exist" alongside a directory only orbit can
			// see leaves them with no command that works.
			if removed, rmErr := instance.RemoveResidue(baseHome, name); rmErr != nil {
				return rmErr
			} else if removed {
				return reportInstanceCleaned(name)
			}
			return cli.NewInvalidArgumentError(fmt.Sprintf("instance %q does not exist", name))
		}
		return err
	}
	if _, err := instance.Activate(name); err != nil {
		return cli.NewInvalidArgumentError(err.Error())
	}
	skipped, err := stopInstanceRuntime(context.Background())
	if err != nil {
		return err
	}
	if err := container.PurgeNamespace(context.Background(), manifest.Namespace); err != nil {
		return fmt.Errorf("removing instance Docker resources: %w", err)
	}
	if err := instance.RemoveHome(baseHome, name); err != nil {
		return fmt.Errorf("removing instance state: %w", err)
	}
	if err := sweepInstanceHome(baseHome, name); err != nil {
		fmt.Fprintf(os.Stderr, "note: instance home for %q left behind: %v\n", name, err)
	}
	return reportInstanceCleaned(name, skipped...)
}

// skippedProcess is a recorded PID that clean left running because the live
// process no longer provably belongs to the instance.
type skippedProcess struct {
	Service string `json:"service"`
	PID     int    `json:"pid"`
	Reason  string `json:"reason"`
}

// stopInstanceRuntime stops what the activated instance still runs using only
// its home, never its config: an old instance's config often lived in a
// checkout or temp directory that no longer exists. A healthy daemon stops its
// own resources. Without one, the processes a dead daemon recorded are retired
// directly, since starting a daemon just to clean would need that config.
func stopInstanceRuntime(ctx context.Context) ([]skippedProcess, error) {
	client, err := daemon.ConnectRunningDaemon(ctx)
	if err != nil {
		return nil, renderDaemonStartError(err)
	}
	if client == nil {
		return retireRecordedProcesses()
	}
	if _, err := client.DownAndWait(); err != nil {
		return nil, fmt.Errorf("stopping instance resources: %w", err)
	}
	if pid, alive := daemon.IsDaemonRunning(); alive {
		if _, err := stopDaemon(pid); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// retireRecordedProcesses stops service processes a dead daemon left in
// state.json. Graceful shutdown persists state before stopping services, so
// these PIDs are usually stale and may by now belong to unrelated processes;
// process.RetireRecorded signals only the ones it can prove are the service.
func retireRecordedProcesses() ([]skippedProcess, error) {
	state, err := daemonsrv.NewStateFile(daemonsrv.DefaultStatePath()).Read()
	if err != nil {
		return nil, fmt.Errorf("reading instance state: %w", err)
	}
	names := make([]string, 0, len(state.Processes))
	for name := range state.Processes {
		names = append(names, name)
	}
	sort.Strings(names)
	var skipped []skippedProcess
	for _, name := range names {
		record := state.Processes[name]
		_, err := process.RetireRecorded(process.RecordedProcess{
			Name: name, PID: record.PID, PGID: record.PGID, Command: record.Command, Dir: record.Dir,
		}, config.DefaultShutdownTimeout)
		var mismatch *process.OwnershipMismatchError
		switch {
		case errors.As(err, &mismatch):
			skipped = append(skipped, skippedProcess{Service: name, PID: record.PID, Reason: mismatch.Reason})
		case err != nil:
			// One stuck group must not keep the instance's Docker resources
			// and home around; report it alongside the unproven ones.
			skipped = append(skipped, skippedProcess{Service: name, PID: record.PID, Reason: err.Error()})
		}
	}
	return skipped, nil
}

// sweepInstanceHome removes the empty instance directory, retrying for the
// case where clean stopped a running daemon.
//
// One pass is not enough then. waitForDaemonStop returns when the pid stops
// being alive, and a daemon can still land a last write after that — so the
// sweep can find nothing, and the directory appears a moment later. When no
// daemon was running the first passes already find the directory gone.
//
// Retrying beats waiting longer because there is nothing to wait *for* that
// the process table exposes. A few short passes cover the window; if the
// directory is still there afterwards it is not this race, and saying so is
// better than blocking a clean that already removed everything of substance.
func sweepInstanceHome(baseHome, name string) error {
	var lastErr error
	clearFor := 0
	for attempt := 0; attempt < 8; attempt++ {
		if attempt > 0 {
			time.Sleep(50 * time.Millisecond)
		}
		removed, err := instance.RemoveEmptyHome(baseHome, name)
		if err != nil {
			lastErr = err
			clearFor = 0
			continue
		}
		if removed {
			clearFor = 0
			continue
		}
		// Absent this pass. Two in a row means the late write has landed and
		// been swept, rather than not having happened yet.
		clearFor++
		if clearFor >= 2 {
			return nil
		}
	}
	return lastErr
}

func reportInstanceCleaned(name string, skipped ...skippedProcess) error {
	if cli.JSONOutput {
		data := map[string]any{"instance": name, "removed": true}
		if len(skipped) > 0 {
			data["skipped_processes"] = skipped
		}
		return cli.WriteJSONSuccess(os.Stdout, commandString(), data, nil)
	}
	for _, item := range skipped {
		fmt.Fprintf(os.Stderr, "note: left pid %d running for service %q (%s); stop it yourself if it is still that service\n", item.PID, item.Service, item.Reason)
	}
	fmt.Printf("Cleaned instance %q.\n", name)
	return nil
}
