package app

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/spf13/cobra"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check system health and diagnose common issues",
		RunE:  runDoctor,
	}
}

func runDoctor(_ *cobra.Command, _ []string) error {
	return runDoctorWithOptions(doctorOptions{showDaemon: true})
}

type doctorOptions struct {
	showDaemon bool
}

func runDoctorWithOptions(options doctorOptions) error {
	client := daemon.NewClient(daemon.DefaultSocketPath())
	if client.Health() == nil {
		if status, err := client.Status(); err == nil {
			if mismatch := daemon.CheckConfigMatch(configFile, status.ConfigPath); mismatch != nil {
				return mismatch
			}
		}
	}
	resp := doctorResponse(client)
	failure := doctorFailure(resp, options.showDaemon)
	if cli.JSONOutput {
		if failure != nil {
			if err := cli.WriteJSONFailure(os.Stdout, commandString(), resp, failure, doctorRecommendedActions(resp)); err != nil {
				return err
			}
			return errCLIJSONAlreadyRendered{err: failure}
		}
		return cli.WriteJSONSuccess(os.Stdout, commandString(), resp, doctorRecommendedActions(resp))
	}
	_, _ = cli.Bold.Println("Checks:")
	for _, c := range resp.Checks {
		if !options.showDaemon && c.Name == "Daemon" {
			continue
		}
		icon := cli.Faint.Sprint("—")
		switch c.Status {
		case daemon.CheckPass:
			icon = cli.Green.Sprint("✓")
		case daemon.CheckFail:
			icon = cli.Red.Sprint("✗")
		case daemon.CheckWarn:
			icon = cli.Yellow.Sprint("!")
		}
		fmt.Printf("  %s %s: %s\n", icon, c.Name, c.Message)
		if c.Hint != "" && (c.Status == daemon.CheckFail || c.Status == daemon.CheckWarn) {
			_, _ = cli.Faint.Printf("      → %s\n", c.Hint)
		}
	}
	return failure
}

func doctorFailure(resp *daemon.DoctorResponse, showDaemon bool) error {
	var failed []string
	if resp != nil {
		for _, check := range resp.Checks {
			if !showDaemon && check.Name == "Daemon" {
				continue
			}
			if check.Status == daemon.CheckFail {
				failed = append(failed, check.Name)
			}
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return cli.NewChecksFailedError(fmt.Sprintf("doctor found %d failed check(s): %s", len(failed), strings.Join(failed, ", ")))
}

func doctorResponse(client *daemon.Client) *daemon.DoctorResponse {
	if client.Health() == nil {
		if status, err := client.Status(); err == nil {
			if daemon.CheckConfigMatch(configFile, status.ConfigPath) == nil {
				if resp, err := client.Doctor(); err == nil {
					return resp
				}
			}
		}
	}
	return localDoctorResponse()
}

func localDoctorResponse() *daemon.DoctorResponse {
	var checks []daemon.DoctorCheck
	var cfg *config.Config
	if loaded, err := config.Load(configFile); err != nil {
		checks = append(checks, daemon.DoctorCheck{Name: "Config", Status: daemon.CheckFail, Message: err.Error()})
	} else if err := config.Validate(loaded); err != nil {
		checks = append(checks, daemon.DoctorCheck{Name: "Config", Status: daemon.CheckFail, Message: err.Error()})
	} else {
		cfg = loaded
		checks = append(checks, daemon.DoctorCheck{Name: "Config", Status: daemon.CheckPass, Message: configFile})
		if len(cfg.Containers) > 0 {
			checks = append(checks, daemonsrv.DockerCheck())
		}
		checks = append(checks, localPortChecks(cfg)...)
		checks = append(checks, daemonsrv.HostEnvironmentChecks(cfg)...)
	}
	checks = append(checks, daemon.DoctorCheck{Name: "Daemon", Status: daemon.CheckInfo, Message: "not running"})
	// Feature-owned offline checks (the DB workflow) come from the
	// extensions' CLIDoctor hooks; a nil cfg is reported by the Config
	// fail check above, so features aren't asked to evaluate it.
	if cfg != nil {
		for _, ext := range extensions {
			if ext.CLIDoctor != nil {
				checks = append(checks, ext.CLIDoctor.Checks(cfg)...)
			}
		}
	}
	return &daemon.DoctorResponse{
		Checks: checks,
		RanAt:  time.Now().UTC().Format(time.RFC3339),
	}
}

func localPortChecks(cfg *config.Config) []daemon.DoctorCheck {
	type portEntry struct {
		port int
		name string
	}
	var ports []portEntry
	for name, container := range cfg.Containers {
		for _, port := range container.Ports {
			ports = append(ports, portEntry{port: port.Host, name: name})
		}
	}
	for name, service := range cfg.Services {
		for _, port := range service.Ports {
			ports = append(ports, portEntry{port: port.Host, name: name})
		}
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].port < ports[j].port })
	checks := make([]daemon.DoctorCheck, 0, len(ports))
	for _, entry := range ports {
		check := daemon.DoctorCheck{
			Name:    fmt.Sprintf("Port %d", entry.port),
			Status:  daemon.CheckPass,
			Message: "available (" + entry.name + ")",
		}
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", entry.port))
		if err != nil {
			check.Status = daemon.CheckFail
			check.Message = "already in use (needed by " + entry.name + ")"
			check.Hint = fmt.Sprintf("Stop the process using port %d or change %s's host port.", entry.port, entry.name)
		} else {
			_ = listener.Close()
		}
		checks = append(checks, check)
	}
	return checks
}

func doctorRecommendedActions(resp *daemon.DoctorResponse) []cli.JSONAction {
	actions := []cli.JSONAction{cli.StatusAction()}
	if resp == nil {
		return append(actions, cli.DoctorAction())
	}
	added := map[string]bool{"orbit status --json": true}
	for _, check := range resp.Checks {
		if check.Status == daemon.CheckFail || check.Status == daemon.CheckWarn {
			if !added["orbit doctor --json"] {
				actions = append(actions, cli.DoctorAction())
				added["orbit doctor --json"] = true
			}
			if cmd, ok := strings.CutPrefix(check.Hint, "run: "); ok {
				cmd = strings.TrimSpace(cmd)
				if strings.HasPrefix(cmd, "orbit ") && !strings.Contains(cmd, " --json") {
					cmd += " --json"
				}
				if cmd != "" && !added[cmd] {
					actions = append(actions, cli.JSONAction{
						Command:     cmd,
						Reason:      "Apply doctor hint for " + check.Name + ".",
						Destructive: false,
					})
					added[cmd] = true
				}
			}
		}
	}
	return actions
}
