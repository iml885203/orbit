package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/container"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/instance"
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
		instances[i].Endpoints = instanceEndpoints(instances[i])
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
	fmt.Printf("%-20s %-18s %-10s %s\n", "NAME", "ENVIRONMENT", "STATE", "ENDPOINTS")
	for _, item := range instances {
		fmt.Printf("%-20s %-18s %-10s %s\n", item.Name, item.Environment, item.State, formatInstanceEndpoints(item.Endpoints))
	}
	return nil
}

func instanceEndpoints(item instance.Summary) map[string]string {
	endpoints := make(map[string]string)
	if item.Dashboard != "" {
		endpoints["dashboard"] = item.Dashboard
	}
	if item.State != "running" {
		return endpoints
	}
	status, err := daemon.NewClient(item.SocketPath).Status()
	if err != nil {
		return endpoints
	}
	for _, resource := range status.Resources {
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
	return endpoints
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
	client, err := daemon.EnsureDaemon(manifest.ConfigPath, nil)
	if err != nil {
		return renderDaemonStartError(err)
	}
	if _, err := client.DownAndWait(); err != nil {
		return fmt.Errorf("stopping instance resources: %w", err)
	}
	pid, alive := daemon.IsDaemonRunning()
	if alive {
		if _, err := stopDaemon(pid); err != nil {
			return err
		}
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
	return reportInstanceCleaned(name)
}

// sweepInstanceHome removes the empty directory clean's own Activate created
// so the daemon could be addressed.
//
// One pass is not enough. waitForDaemonStop returns when the pid stops being
// alive, and a daemon can still land a last write after that — so the sweep
// can find nothing, and the directory appears a moment later. Only instances
// that actually ran a daemon show it: one created but never started cleans in
// a single pass.
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

func reportInstanceCleaned(name string) error {
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), map[string]any{"instance": name, "removed": true}, nil)
	}
	fmt.Printf("Cleaned instance %q.\n", name)
	return nil
}
