package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

type projectContextSwitch struct {
	FromName         string   `json:"from_name"`
	FromPath         string   `json:"from_path,omitempty"`
	ToName           string   `json:"to_name"`
	ToPath           string   `json:"to_path"`
	StoppedResources []string `json:"stopped_resources"`
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
	if !usesDiscoveredProjectConfig(configFile) {
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
	if !cli.JSONOutput {
		if len(switched.StoppedResources) == 0 {
			fmt.Printf("Switching from %s to %s...\n", switched.FromName, switched.ToName)
		} else {
			fmt.Printf(
				"Switching from %s to %s; stopping %d running resource(s)...\n",
				switched.FromName,
				switched.ToName,
				len(switched.StoppedResources),
			)
		}
	}
	if _, err := stopDaemon(previousPID); err != nil {
		return nil, fmt.Errorf("stopping %s before project switch: %w", switched.FromName, err)
	}
	return switched, nil
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
