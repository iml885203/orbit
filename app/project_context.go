package app

import (
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
	return cli.WithJSONActions(projectContextInactiveError{
		currentName: currentName,
		runningName: runningName,
	}, []cli.JSONAction{{
		Command:     "orbit up --json",
		Reason:      fmt.Sprintf("Stop %s and start %s.", runningName, currentName),
		Destructive: false,
	}})
}

func switchDiscoveredProjectContext() (*projectContextSwitch, error) {
	if environmentContextKind(configFile) != "project" {
		return nil, nil
	}
	previousPID, alive := daemon.IsDaemonRunning()
	if !alive {
		return nil, nil
	}
	client := daemon.NewClient(daemon.DefaultSocketPath())
	status, err := client.Status()
	if err != nil {
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
