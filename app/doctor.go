package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/shellquote"
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

func runDoctor(cmd *cobra.Command, _ []string) error {
	return runDoctorWithOptions(doctorOptions{
		showDaemon:     true,
		explicitConfig: cmd.Root().PersistentFlags().Changed("config"),
	})
}

type doctorOptions struct {
	showDaemon     bool
	explicitConfig bool
}

type setupRequiredError struct{}

func (setupRequiredError) Error() string {
	return "Orbit setup is required — run 'orbit init'"
}

func (setupRequiredError) ErrorCode() string {
	return "setup_required"
}

func (setupRequiredError) CLIJSONHint() string {
	return "Set up Orbit before running environment diagnostics."
}

func setupRequired(selection environmentSelection, path string) bool {
	if selection.State != environmentSelectionNone || configFileExists(path) {
		return false
	}
	if path == "" {
		return true
	}
	return distribution.DefaultEnv != "" &&
		sameFilePath(path, filepath.Join(envsDestDir(), distribution.DefaultEnv))
}

func runDoctorWithOptions(options doctorOptions) error {
	client := daemon.NewClient(daemon.DefaultSocketPath())
	var detachedStatus *daemon.StatusResponse
	if client.Health() == nil {
		if status, err := client.Status(); err == nil {
			if shouldResumeDetachedProject(options.explicitConfig, configFile, status.ConfigPath) {
				configFile = status.ConfigPath
				detachedStatus = status
			} else if mismatch := daemon.CheckConfigMatch(configFile, status.ConfigPath); mismatch != nil {
				if !usesDiscoveredProjectConfig(configFile) {
					return mismatch
				}
			}
		}
	}
	var resp *daemon.DoctorResponse
	if detachedStatus != nil {
		resp = localDoctorResponseWithContext(true, detachedStatus)
	} else {
		resp = doctorResponse(client)
	}
	failure := doctorFailure(resp, options.showDaemon)
	var schemaMismatch *config.SchemaVersionMismatchError
	if failure != nil {
		if _, err := config.Load(configFile); errors.As(err, &schemaMismatch) {
			failure = schemaMismatch
		}
	}
	if cli.JSONOutput {
		if failure != nil {
			if schemaMismatch != nil {
				if err := cli.WriteJSONFailure(os.Stdout, commandString(), resp, failure, nil); err != nil {
					return err
				}
				return errCLIJSONAlreadyRendered{err: failure}
			}
			actions := doctorRecommendedActions(resp)
			failure = cli.WithJSONReplacementActions(failure, actions)
			if err := cli.WriteJSONFailure(os.Stdout, commandString(), resp, failure, actions); err != nil {
				return err
			}
			return errCLIJSONAlreadyRendered{err: failure}
		}
		return cli.WriteJSONSuccess(os.Stdout, commandString(), resp, doctorRecommendedActions(resp))
	}
	summary := summarizeDoctorForHuman(resp, options.showDaemon)
	if len(summary.attention) > 0 {
		_, _ = cli.Bold.Println("Needs attention:")
		for _, item := range summary.attention {
			printHumanDoctorCheck(item.label, item.check)
		}
	} else {
		_, _ = cli.Green.Println("✓ Environment is ready")
	}
	if summary.passed > 0 {
		_, _ = cli.Faint.Printf("  ✓ %d %s passed\n", summary.passed, pluralize(summary.passed, "check", "checks"))
	}
	for _, item := range summary.context {
		fmt.Printf("  %s %s: %s\n", cli.Faint.Sprint("—"), item.label, item.check.Message)
		if item.check.Hint != "" {
			_, _ = cli.Faint.Printf("      → %s\n", item.check.Hint)
		}
	}
	if options.showDaemon && failure == nil {
		if next := doctorStartCommand(resp); next != "" {
			fmt.Println("  Next: " + next)
		}
	}
	return failure
}

type humanDoctorItem struct {
	label string
	check daemon.DoctorCheck
}

type humanDoctorSummary struct {
	attention []humanDoctorItem
	context   []humanDoctorItem
	passed    int
}

func summarizeDoctorForHuman(resp *daemon.DoctorResponse, showDaemon bool) humanDoctorSummary {
	var summary humanDoctorSummary
	if resp == nil {
		return summary
	}
	for _, check := range resp.Checks {
		label, visible := humanDoctorCheck(check, showDaemon)
		if !visible {
			continue
		}
		item := humanDoctorItem{label: label, check: check}
		switch check.Status {
		case daemon.CheckFail, daemon.CheckWarn:
			summary.attention = append(summary.attention, item)
		case daemon.CheckPass:
			summary.passed++
		default:
			summary.context = append(summary.context, item)
		}
	}
	return summary
}

func printHumanDoctorCheck(label string, check daemon.DoctorCheck) {
	icon := cli.Faint.Sprint("—")
	switch check.Status {
	case daemon.CheckPass:
		icon = cli.Green.Sprint("✓")
	case daemon.CheckFail:
		icon = cli.Red.Sprint("✗")
	case daemon.CheckWarn:
		icon = cli.Yellow.Sprint("!")
	}
	fmt.Printf("  %s %s: %s\n", icon, label, check.Message)
	if check.Hint != "" {
		_, _ = cli.Faint.Printf("      → %s\n", check.Hint)
	}
}

func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func humanDoctorCheck(check daemon.DoctorCheck, showDaemon bool) (string, bool) {
	if check.Name != "Daemon" {
		if check.Status == daemon.CheckInfo {
			return "", false
		}
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
	if len(failed) == 1 && failed[0] == "Setup" {
		return setupRequiredError{}
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
			} else if usesDiscoveredProjectConfig(configFile) {
				return localDoctorResponseWithContext(true, status)
			}
		}
	}
	return localDoctorResponse()
}

func localDoctorResponse() *daemon.DoctorResponse {
	return localDoctorResponseWithDaemon(false)
}

func localDoctorResponseWithDaemon(daemonRunning bool) *daemon.DoctorResponse {
	return localDoctorResponseWithContext(daemonRunning, nil)
}

func localDoctorResponseWithContext(
	daemonRunning bool,
	runningStatus *daemon.StatusResponse,
) *daemon.DoctorResponse {
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
	} else if setupRequired(selection, configFile) {
		checks = append(checks, daemon.DoctorCheck{
			Name:    "Setup",
			Status:  daemon.CheckFail,
			Message: "Orbit is not set up yet",
			Hint:    "run: orbit init",
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
		checks = append(checks, localPortChecksWithContext(cfg, runningStatus)...)
		checks = append(checks, daemonsrv.HostEnvironmentChecks(cfg)...)
	}
	daemonMessage := "not running"
	if daemonRunning {
		daemonMessage = "running with the previous environment snapshot"
	}
	daemonCheck := daemon.DoctorCheck{Name: "Daemon", Status: daemon.CheckInfo, Message: daemonMessage}
	if runningStatus != nil {
		if sameFilePath(configFile, runningStatus.ConfigPath) {
			daemonCheck.Message = fmt.Sprintf(
				"%s is still active; run commands from %s or pass --config %s",
				projectContextName(runningStatus.ConfigPath),
				filepath.Dir(runningStatus.ConfigPath),
				shellquote.Quote(runningStatus.ConfigPath),
			)
		} else {
			daemonCheck.Message = fmt.Sprintf(
				"%s is running; orbit up switches to %s",
				projectContextName(runningStatus.ConfigPath),
				projectContextName(configFile),
			)
			daemonCheck.Hint = "run: orbit up"
		}
	} else if !daemonRunning && cfg != nil && len(cfg.Containers)+len(cfg.Services) > 0 {
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
	return localPortChecksWithContext(cfg, nil)
}

func localPortChecksWithContext(
	cfg *config.Config,
	runningStatus *daemon.StatusResponse,
) []daemon.DoctorCheck {
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
	releasedOnSwitch := map[int]bool{}
	runningName := ""
	if runningStatus != nil {
		releasedOnSwitch = projectContextPorts(runningStatus.Resources)
		runningName = projectContextName(runningStatus.ConfigPath)
	}
	for _, entry := range ports {
		check := daemon.DoctorCheck{
			Name:    fmt.Sprintf("Port %d", entry.port),
			Status:  daemon.CheckPass,
			Message: "available (" + entry.name + ")",
		}
		conflicts := port.CheckPorts(map[string][]int{entry.name: {entry.port}})
		if len(conflicts) > 0 {
			if releasedOnSwitch[entry.port] {
				check.Status = daemon.CheckInfo
				check.Message = fmt.Sprintf(
					"used by %s; Orbit will release it when switching projects",
					runningName,
				)
				checks = append(checks, check)
				continue
			}
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
			if check.Name == "Setup" {
				return []cli.JSONAction{{
					Command: "orbit init --yes --json",
					Reason:  "Set up Orbit and select an environment.",
				}}
			}
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
	return nil
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
