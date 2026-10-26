package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
)

type projectContextSwitch struct {
	FromName         string   `json:"from_name"`
	FromPath         string   `json:"from_path,omitempty"`
	ToName           string   `json:"to_name"`
	ToPath           string   `json:"to_path"`
	StoppedResources []string `json:"stopped_resources"`
}

var projectContextYes bool

// configFlagGiven records that this invocation named its config with -c, as
// opposed to resolving one; configContextKind cannot tell, because a resolved
// fallback config is inferred as "explicit" too.
var configFlagGiven bool
var errProjectContextSwitchDeclined = errors.New("project context switch declined")

func environmentContextKind(configPath string) string {
	if configContextKind != "" && sameFilePath(configPath, configFile) {
		return configContextKind
	}
	return inferredEnvironmentContextKind(configPath)
}

func inferredEnvironmentContextKind(configPath string) string {
	if usesDiscoveredProjectConfig(configPath) {
		return "project"
	}
	if selected := readCurrentEnv(); selected != "" && sameFilePath(selected, configPath) {
		return "managed"
	}
	return "explicit"
}

type projectContextInactiveError struct {
	currentName string
	runningName string
}

func (e projectContextInactiveError) Error() string {
	return fmt.Sprintf(
		"%s is not running; %s is still active — run 'orbit up' to switch projects",
		e.currentName,
		e.runningName,
	)
}

func (e projectContextInactiveError) ErrorCode() string {
	return "project_context_inactive"
}

func (e projectContextInactiveError) CLIJSONHint() string {
	return "Run orbit up from this project to stop the previous environment and start this one."
}

func projectContextInactive(configPath, runningPath string) error {
	currentName := projectContextName(configPath)
	runningName := projectContextName(runningPath)
	currentLabel, runningLabel := currentName, runningName
	if currentName == runningName {
		currentLabel = fmt.Sprintf("%s (%s)", currentName, configPath)
		runningLabel = fmt.Sprintf("%s (%s)", runningName, runningPath)
	}
	return cli.WithJSONActions(projectContextInactiveError{
		currentName: currentLabel,
		runningName: runningLabel,
	}, []cli.JSONAction{{
		Command:     "orbit up --json",
		Reason:      fmt.Sprintf("Stop %s and start %s.", runningLabel, currentLabel),
		Destructive: false,
	}})
}

// switchRunningContext lets `up` move the daemon to a discovered project or a
// config named with -c: starting that config is the whole point of the
// command, so it confirms what it stops instead of failing closed like every
// other command does on a mismatch. A bare `up` that merely resolved to a
// managed or fallback config is not a request to replace what is running.
func switchRunningContext(ctx context.Context) (*projectContextSwitch, error) {
	if !configFlagGiven && environmentContextKind(configFile) != "project" {
		return nil, nil
	}
	previousPID, alive := daemon.IsDaemonRunning()
	if !alive {
		return nil, nil
	}
	client := daemon.NewClient(daemon.DefaultSocketPath()).WithContext(ctx)
	status, err := client.Status()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// A recorded PID that does not answer is EnsureDaemon's to judge: it
		// retires a stale daemon without touching an unrelated process that
		// reused the PID. A daemon that answers but cannot report status must
		// not be mistaken for one serving the requested config.
		if client.Health() != nil {
			return nil, nil
		}
		return nil, fmt.Errorf("checking running project: %w", err)
	}
	mismatch := daemon.CheckConfigMatch(configFile, status.ConfigPath)
	if mismatch == nil {
		return nil, nil
	}
	var configMismatch *daemon.ConfigMismatchError
	if !errors.As(mismatch, &configMismatch) {
		return nil, mismatch
	}

	switched := &projectContextSwitch{
		FromName:         projectContextName(status.ConfigPath),
		FromPath:         status.ConfigPath,
		ToName:           projectContextName(configFile),
		ToPath:           configFile,
		StoppedResources: runningEnvironmentResources(status.Resources),
	}
	fromLabel, toLabel := projectContextSwitchLabels(switched)
	if len(switched.StoppedResources) > 0 && !projectContextYes {
		summary := fmt.Sprintf(
			"Switching from %s to %s stops %d running resource(s): %s.",
			fromLabel,
			toLabel,
			len(switched.StoppedResources),
			strings.Join(switched.StoppedResources, ", "),
		)
		if cli.JSONOutput || !cli.CanPrompt() {
			nextCommand := commandString()
			if strings.HasSuffix(nextCommand, " --json") {
				nextCommand = strings.TrimSuffix(nextCommand, " --json") + " --yes --json"
			} else {
				nextCommand += " --yes"
			}
			return nil, cli.WithJSONReplacementActions(
				cli.NewConfirmationRequiredError(summary+" Rerun with --yes to confirm."),
				[]cli.JSONAction{{
					Command:     nextCommand,
					Reason:      "Confirm stopping the running resources before switching contexts.",
					Destructive: true,
				}},
			)
		}
		if !cli.Confirm(summary + " Continue?") {
			return nil, errProjectContextSwitchDeclined
		}
	}
	if !cli.JSONOutput {
		if len(switched.StoppedResources) == 0 {
			fmt.Printf("Switching from %s to %s...\n", fromLabel, toLabel)
		} else {
			fmt.Printf(
				"Switching from %s to %s; stopping %d running resource(s)...\n",
				fromLabel,
				toLabel,
				len(switched.StoppedResources),
			)
		}
	}
	// Background, not ctx: once confirmed, a stop that Ctrl-C abandoned
	// halfway would leave neither environment running.
	if _, err := stopDaemon(previousPID); err != nil {
		return nil, fmt.Errorf("stopping %s before project switch: %w", switched.FromName, err)
	}
	return switched, nil
}

func printProjectEnvironmentContext() {
	if cli.JSONOutput || environmentContextKind(configFile) != "project" {
		return
	}
	fmt.Printf("Using project environment: %s\n", projectContextName(configFile))
	fmt.Printf("Config:       %s\n", configFile)
	fmt.Printf("Project root: %s\n", filepath.Dir(configFile))
	if selected := readCurrentEnv(); selected != "" && !sameFilePath(selected, configFile) {
		fmt.Printf(
			"Managed environment %s remains selected but is not active.\n",
			daemonsrv.EnvShortName(selected),
		)
	}
}

func projectContextSwitchLabels(switched *projectContextSwitch) (string, string) {
	if switched.FromName != switched.ToName {
		return switched.FromName, switched.ToName
	}
	return fmt.Sprintf("%s (%s)", switched.FromName, switched.FromPath),
		fmt.Sprintf("%s (%s)", switched.ToName, switched.ToPath)
}

func projectContextName(configPath string) string {
	cleaned := filepath.Clean(strings.TrimSpace(configPath))
	if cleaned == "." || cleaned == "" {
		return "previous environment"
	}
	if strings.EqualFold(filepath.Base(cleaned), projectConfigName) {
		name := filepath.Base(filepath.Dir(cleaned))
		if name != "." && name != string(filepath.Separator) && name != "" {
			return name
		}
	}
	name := strings.TrimSuffix(filepath.Base(cleaned), filepath.Ext(cleaned))
	if name == "" {
		return "previous environment"
	}
	return name
}

func projectContextPorts(resources []daemon.ResourceStatus) map[int]bool {
	ports := make(map[int]bool)
	for i := range resources {
		for _, portNumber := range resources[i].Ports {
			if portNumber > 0 {
				ports[portNumber] = true
			}
		}
	}
	return ports
}

// upCommandFor is the command that brings configPath up even while another
// config runs: a discovered project switches with a bare up, every other
// config is named with -c, the one form up switches for.
func upCommandFor(configPath string) string {
	if configPath == "" || environmentContextKind(configPath) == "project" {
		return "orbit up"
	}
	return (&daemon.ConfigMismatchError{Requested: configPath}).SwitchCommand()
}

// startCommand starts the config this invocation used while nothing else
// runs: a config named with -c has to be named again, while a resolved one is
// what a bare up resolves too.
func startCommand() string {
	if configFlagGiven && configFile != "" {
		return (&daemon.ConfigMismatchError{Requested: configFile}).SwitchCommand()
	}
	return "orbit up"
}
