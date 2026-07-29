package app

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/port"
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
			actions := doctorRecommendedActions(resp)
			failure = cli.WithJSONReplacementActions(failure, actions)
			if err := cli.WriteJSONFailure(os.Stdout, commandString(), resp, failure, actions); err != nil {
				return err
			}
			return errCLIJSONAlreadyRendered{err: failure}
		}
		return cli.WriteJSONSuccess(os.Stdout, commandString(), resp, doctorRecommendedActions(resp))
	}
	_, _ = cli.Bold.Println("Checks:")
	for _, c := range resp.Checks {
		label, visible := humanDoctorCheck(c, options.showDaemon)
		if !visible {
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
		fmt.Printf("  %s %s: %s\n", icon, label, c.Message)
		if c.Hint != "" && (c.Status == daemon.CheckFail || c.Status == daemon.CheckWarn) {
			_, _ = cli.Faint.Printf("      → %s\n", c.Hint)
		}
	}
	if options.showDaemon && failure == nil {
		if next := doctorStartCommand(resp); next != "" {
			fmt.Println("  Next: " + next)
		}
	}
	return failure
}

func humanDoctorCheck(check daemon.DoctorCheck, showDaemon bool) (string, bool) {
	if check.Name != "Daemon" {
		return check.Name, true
	}
	if !showDaemon || (check.Status == daemon.CheckInfo && check.Message == "not running") {
		return "", false
	}
	return "Environment", true
}

func doctorFailure(resp *daemon.DoctorResponse, showDaemon bool) error {
	var failed []string
	if resp != nil {
		for _, check := range resp.Checks {
			if !showDaemon && check.Name == "Daemon" {
				continue
			}
			if check.Status == daemon.CheckFail {
				name, _ := humanDoctorCheck(check, showDaemon)
				failed = append(failed, name)
			}
		}
	}
	if len(failed) == 0 {
		return nil
	}
	if len(failed) == 1 && failed[0] == "Environment selection" {
		return newEnvironmentSelectionRequiredError(readEnvironmentSelection())
	}
	if len(failed) == 1 && failed[0] == "Orbit update" {
		return &daemon.UpdateRequiredError{
			RestartCommand:     orbitRestartCommand(false),
			RestartJSONCommand: orbitRestartCommand(true),
		}
	}
	for _, check := range resp.Checks {
		if check.Status == daemon.CheckFail && strings.HasPrefix(check.Name, "Working directory (") {
			return cli.NewServiceWorkingDirectoryError(check.Message)
		}
	}
	return cli.NewChecksFailedError(fmt.Sprintf("doctor found %d failed check(s): %s", len(failed), strings.Join(failed, ", ")))
}

func doctorResponse(client *daemon.Client) *daemon.DoctorResponse {
	if environmentSelectionBlocksConfig(readEnvironmentSelection(), configFile) {
		return localDoctorResponseWithDaemon(client.Health() == nil)
	}
	if client.Health() == nil {
		if status, err := client.Status(); err == nil {
			if daemon.CheckConfigMatch(configFile, status.ConfigPath) == nil {
				if resp, err := client.Doctor(); err == nil {
					if version, versionErr := currentDaemonVersion(client); versionErr == nil {
						return addUpdateDoctorCheck(resp, version)
					}
					return resp
				}
			}
		}
	}
	return localDoctorResponse()
}

func localDoctorResponse() *daemon.DoctorResponse {
	return localDoctorResponseWithDaemon(false)
}

func localDoctorResponseWithDaemon(daemonRunning bool) *daemon.DoctorResponse {
	var checks []daemon.DoctorCheck
	var cfg *config.Config
	selection := readEnvironmentSelection()
	if environmentSelectionBlocksConfig(selection, configFile) {
		hint := "run: orbit env sync"
		if len(selection.Environments) == 1 {
			hint = "run: " + environmentSwitchCommand(selection.Environments[0].Name, false)
		} else if len(selection.Environments) > 1 {
			hint = "run: orbit env list"
		}
		checks = append(checks, daemon.DoctorCheck{
			Name:    "Environment selection",
			Status:  daemon.CheckFail,
			Message: fmt.Sprintf("environment %q is no longer available", selection.SelectedName),
			Hint:    hint,
		})
	} else if loaded, err := config.Load(configFile); err != nil {
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
	daemonMessage := "not running"
	if daemonRunning {
		daemonMessage = "running with the previous environment snapshot"
	}
	daemonCheck := daemon.DoctorCheck{Name: "Daemon", Status: daemon.CheckInfo, Message: daemonMessage}
	if !daemonRunning && cfg != nil && len(cfg.Containers)+len(cfg.Services) > 0 {
		daemonCheck.Hint = "run: orbit up"
	}
	checks = append(checks, daemonCheck)
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
		auto bool
	}
	var ports []portEntry
	for name, container := range cfg.Containers {
		for _, definition := range container.Ports {
			ports = append(ports, portEntry{port: definition.Host, name: name, auto: definition.IsAuto()})
		}
	}
	for name, service := range cfg.Services {
		for _, definition := range service.Ports {
			ports = append(ports, portEntry{port: definition.Host, name: name, auto: definition.IsAuto()})
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
		conflicts := port.CheckPorts(map[string][]int{entry.name: {entry.port}})
		if len(conflicts) > 0 {
			if entry.auto {
				check.Status = daemon.CheckInfo
				check.Message = fmt.Sprintf(
					"preferred port is occupied (%s); Orbit will choose an available port when it starts",
					entry.name,
				)
				checks = append(checks, check)
				continue
			}
			conflict := port.NewConflictError(conflicts[0])
			check.Status = daemon.CheckFail
			check.Message = conflict.Error()
			check.Hint = "run: " + conflict.InspectCommand
		}
		checks = append(checks, check)
	}
	return checks
}

func doctorRecommendedActions(resp *daemon.DoctorResponse) []cli.JSONAction {
	if resp != nil {
		for _, check := range resp.Checks {
			if check.Name == "Environment selection" {
				return environmentSelectionActions(readEnvironmentSelection())
			}
			if check.Name == "Orbit update" {
				return []cli.JSONAction{{
					Command: orbitRestartCommand(true),
					Reason:  "Restart Orbit to run the installed version.",
				}}
			}
		}
	}
	if start := doctorStartCommand(resp); start != "" {
		return []cli.JSONAction{{
			Command: start + " --json",
			Reason:  "Start the selected environment.",
		}}
	}
	if actions, onlyPortConflicts := doctorPortConflictActions(resp); onlyPortConflicts {
		return actions
	}
	if resp == nil {
		return []cli.JSONAction{cli.StatusAction()}
	}
	var actions []cli.JSONAction
	added := make(map[string]bool)
	workingDirectoryFailed := false
	for _, check := range resp.Checks {
		if check.Status == daemon.CheckFail || check.Status == daemon.CheckWarn {
			if check.Status == daemon.CheckFail && strings.HasPrefix(check.Name, "Working directory (") {
				workingDirectoryFailed = true
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
	if len(actions) > 0 {
		return actions
	}
	if workingDirectoryFailed {
		return []cli.JSONAction{}
	}
	return []cli.JSONAction{cli.StatusAction()}
}

func doctorReadyToStart(resp *daemon.DoctorResponse) bool {
	return doctorStartCommand(resp) != ""
}

func doctorStartCommand(resp *daemon.DoctorResponse) string {
	if resp == nil {
		return ""
	}
	for _, check := range resp.Checks {
		if check.Status == daemon.CheckFail {
			return ""
		}
	}
	for _, check := range resp.Checks {
		if command, ok := strings.CutPrefix(check.Hint, "run: "); ok &&
			(command == "orbit up" || strings.HasPrefix(command, "orbit up ")) {
			return command
		}
	}
	return ""
}

func doctorPortConflictActions(resp *daemon.DoctorResponse) ([]cli.JSONAction, bool) {
	if resp == nil {
		return nil, false
	}
	var actions []cli.JSONAction
	for _, check := range resp.Checks {
		if check.Status != daemon.CheckFail {
			continue
		}
		if !strings.HasPrefix(check.Name, "Port ") {
			return nil, false
		}
		command, ok := strings.CutPrefix(check.Hint, "run: ")
		if !ok || strings.TrimSpace(command) == "" {
			return nil, false
		}
		actions = append(actions, cli.JSONAction{
			Command:     strings.TrimSpace(command),
			Reason:      "Inspect the process currently using " + check.Name + ".",
			Destructive: false,
		})
	}
	return actions, len(actions) > 0
}

func addUpdateDoctorCheck(resp *daemon.DoctorResponse, version *daemon.VersionResponse) *daemon.DoctorResponse {
	if resp == nil || version == nil || !version.UpdateAvailable {
		return resp
	}
	out := *resp
	out.Checks = append([]daemon.DoctorCheck{{
		Name:    "Orbit update",
		Status:  daemon.CheckFail,
		Message: "an installed Orbit update requires a daemon restart",
		Hint:    "run: " + orbitRestartCommand(false),
	}}, resp.Checks...)
	return &out
}
