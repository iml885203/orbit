package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/spf13/cobra"
)

const inspectJSONSchemaVersion = "orbit.inspect.v2"

const (
	inspectReadinessSetupRequired     = "setup_required"
	inspectReadinessSelectionRequired = "selection_required"
	inspectReadinessConfigInvalid     = "config_invalid"
	inspectReadinessNeedsDaemon       = "needs_daemon"
	inspectReadinessUpdateRequired    = "update_required"
	inspectReadinessStopped           = "stopped"
	inspectReadinessDegraded          = "degraded"
	inspectReadinessConverging        = "converging"
	inspectReadinessPartial           = "partial"
	inspectReadinessReady             = "ready"
)

type inspectJSONData struct {
	SchemaVersion      string                `json:"schema_version"`
	Readiness          inspectReadiness      `json:"readiness"`
	Daemon             inspectDaemonSummary  `json:"daemon"`
	Environment        inspectEnvSummary     `json:"environment"`
	Resources          inspectServiceSummary `json:"resources"`
	Risks              []inspectRisk         `json:"risks"`
	RecommendedActions []cli.JSONAction      `json:"recommended_actions"`
}

type inspectReadiness struct {
	State   string `json:"state"`
	Blocked bool   `json:"blocked"`
	Summary string `json:"summary"`
}

type inspectDaemonSummary struct {
	Running         bool   `json:"running"`
	PID             int    `json:"pid,omitempty"`
	Version         string `json:"version,omitempty"`
	OnDisk          string `json:"on_disk,omitempty"`
	OnDiskPath      string `json:"on_disk_path,omitempty"`
	UpdateAvailable bool   `json:"update_available,omitempty"`
	Dashboard       string `json:"dashboard,omitempty"`
}

type inspectEnvSummary struct {
	State                 string                    `json:"state"`
	Source                string                    `json:"source,omitempty"`
	SelectedName          string                    `json:"selected_name,omitempty"`
	SelectedPath          string                    `json:"selected_path,omitempty"`
	Environments          []environmentChoice       `json:"environments"`
	DaemonEnv             string                    `json:"daemon_env,omitempty"`
	ContextSwitchRequired bool                      `json:"context_switch_required,omitempty"`
	RunningName           string                    `json:"running_name,omitempty"`
	RunningPath           string                    `json:"running_path,omitempty"`
	Context               daemon.EnvironmentContext `json:"context"`
}

type inspectServiceSummary struct {
	Total        int                 `json:"total"`
	ByState      map[string][]string `json:"by_state"`
	Degraded     []string            `json:"degraded"`
	Starting     []string            `json:"starting"`
	Stopped      []string            `json:"stopped"`
	failureKinds map[string]string
}

type inspectRisk struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Resource string `json:"resource,omitempty"`
}

type inspectBuildOptions struct {
	Command             string
	ConfigPath          string
	ConfigErr           error
	SetupRequired       bool
	ConfigEnvName       string
	ConfigMatchesDaemon bool
	DaemonEnv           string
	DaemonRunning       bool
	PID                 int
	Version             string
	OnDisk              string
	OnDiskPath          string
	UpdateAvailable     bool
	Dashboard           string
	Status              *daemon.StatusResponse
	StatusErr           error
	Configured          []daemon.ResourceStatus
	ReadinessChecks     []daemon.DoctorCheck
	Selection           environmentSelection
	ContextMismatch     bool
	RunningPath         string
	Context             daemon.EnvironmentContext
}

func inspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "inspect",
		Short:  "Show an agent-ready Orbit state snapshot",
		Hidden: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return inspectTargetError{target: args[0], json: cli.JSONOutput}
		},
		RunE: runInspect,
	}
}

type inspectTargetError struct {
	target string
	json   bool
}

func (e inspectTargetError) Error() string {
	return fmt.Sprintf("inspect reports the whole environment; it does not accept resource %q", e.target)
}

func (e inspectTargetError) ErrorCode() string {
	return "invalid_argument"
}

func (e inspectTargetError) CLIJSONHint() string {
	return "Omit the resource argument to inspect the whole environment."
}

func (e inspectTargetError) nextCommand() string {
	if e.json {
		return "orbit inspect --json"
	}
	return "orbit inspect"
}

func (e inspectTargetError) CLIHumanNextCommand() string {
	return e.nextCommand()
}

func (e inspectTargetError) CLIJSONReplacementActions() []cli.JSONAction {
	return []cli.JSONAction{{
		Command:     e.nextCommand(),
		Reason:      "Inspect the whole environment without a resource argument.",
		Destructive: false,
	}}
}

func runInspect(cmd *cobra.Command, _ []string) error {
	selection := readEnvironmentSelection()
	cfg, cfgErr := config.Load(configFile)
	client := daemon.NewClient(daemon.DefaultSocketPath())
	daemonRunning := client.Health() == nil
	selectionRequired := cfgErr != nil &&
		(environmentSelectionBlocksConfig(selection, configFile) ||
			(selection.State == environmentSelectionNone && len(selection.Environments) > 0))
	setupRequired := cfgErr != nil && !daemonRunning && !configFileExists(configFile) && !selectionRequired
	pid, alive := daemon.IsDaemonRunning()
	if daemonRunning {
		pid = alivePID(pid, alive)
	}

	opts := inspectBuildOptions{
		Command:       commandString(),
		ConfigPath:    configFile,
		ConfigErr:     cfgErr,
		SetupRequired: setupRequired,
		ConfigEnvName: projectContextName(configFile),
		DaemonRunning: daemonRunning,
		PID:           pid,
		Dashboard:     fmt.Sprintf("http://localhost:%d", daemon.DashboardPort()),
		Selection:     activeEnvironmentSelection(selection, configFile),
	}
	if cfg != nil {
		opts.Configured = configuredInspectServices(cfg)
		opts.ReadinessChecks = daemonsrv.DependencyReadinessChecks(cfg)
	}
	if daemonRunning {
		if status, err := client.Status(); err == nil {
			opts.Context = status.Context
			if shouldResumeDetachedProject(cmd.Root().PersistentFlags().Changed("config"), configFile, status.ConfigPath, status.Context.Kind) {
				configFile = status.ConfigPath
				cfg, cfgErr = config.Load(configFile)
				opts.ConfigPath = configFile
				opts.ConfigErr = cfgErr
				opts.ConfigEnvName = projectContextName(configFile)
				opts.Selection = activeEnvironmentSelectionWithKind(selection, configFile, status.Context.Kind)
				if cfg != nil {
					opts.Configured = configuredInspectServices(cfg)
					opts.ReadinessChecks = daemonsrv.DependencyReadinessChecks(cfg)
				}
			}
			if daemon.CheckConfigMatch(configFile, status.ConfigPath) == nil {
				opts.Status = status
				opts.ConfigMatchesDaemon = true
				opts.Selection = activeEnvironmentSelectionWithKind(selection, status.ConfigPath, status.Context.Kind)
			} else if usesDiscoveredProjectConfig(configFile) {
				opts.ContextMismatch = true
				opts.RunningPath = status.ConfigPath
				opts.DaemonEnv = projectContextName(status.ConfigPath)
			} else {
				opts.Status = status
			}
		} else {
			opts.StatusErr = err
		}
		if version, err := currentDaemonVersion(client); err == nil {
			opts.Version = version.Running
			opts.OnDisk = version.OnDisk
			opts.OnDiskPath = version.OnDiskPath
			opts.UpdateAvailable = version.UpdateAvailable
		}
		if opts.DaemonEnv == "" {
			if envs, err := client.Envs(); err == nil {
				opts.DaemonEnv = daemonsrv.EnvShortName(envs.Current)
			}
		}
	}
	if opts.Context.Kind == "" && cfgErr == nil {
		opts.Context = localInspectEnvironmentContext(configFile, opts.Selection)
	}

	data := buildInspectData(opts)
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), data, data.RecommendedActions)
	}
	printInspectHuman(data)
	return nil
}

func localInspectEnvironmentContext(configPath string, selection environmentSelection) daemon.EnvironmentContext {
	identity := configPath
	if absolute, err := filepath.Abs(identity); err == nil {
		identity = absolute
	}
	if resolved, err := filepath.EvalSymlinks(identity); err == nil {
		identity = resolved
	}
	canonicalPath := identity
	kind := environmentContextKind(configPath)
	if _, managedIdentity, managed := managedSourceForPath(identity); managed {
		kind = "managed"
		identity = managedIdentity
	}
	context := daemon.EnvironmentContext{
		Kind: kind, Identity: identity, ConfigPath: canonicalPath,
		DisplayName: daemonsrv.EnvShortName(canonicalPath), Available: true, Running: false,
	}
	if kind == "project" {
		context.DisplayName = projectContextName(identity)
		context.ProjectRoot = filepath.Dir(identity)
	}
	if selection.ManagedSelection != nil {
		context.ManagedSelection = &daemon.ManagedEnvironmentSelection{
			Identity: selection.ManagedSelection.Identity,
			Name:     selection.ManagedSelection.Name,
			Path:     selection.ManagedSelection.Path,
			Active:   kind == "managed" && sameFilePath(selection.ManagedSelection.Path, canonicalPath),
		}
	}
	return context
}

func alivePID(pid int, alive bool) int {
	if !alive {
		return 0
	}
	return pid
}

func buildInspectData(opts inspectBuildOptions) inspectJSONData {
	if opts.Selection.State == "" {
		opts.Selection.State = environmentSelectionNone
	}
	if opts.Selection.Environments == nil {
		opts.Selection.Environments = []environmentChoice{}
	}
	services := emptyInspectServiceSummary()
	if opts.Status != nil {
		services = buildInspectServiceSummary(opts.Status.Resources)
	} else if len(opts.Configured) > 0 {
		services = buildInspectServiceSummary(opts.Configured)
	}
	risks := buildInspectRisks(opts, services)
	readiness := deriveInspectReadiness(opts, services)
	actions := inspectRecommendedActions(readiness, risks, services, opts.ConfigErr, opts.ConfigEnvName, opts.Selection)
	daemonEnv := opts.DaemonEnv
	if opts.ConfigMatchesDaemon && opts.ConfigEnvName != "" {
		daemonEnv = opts.ConfigEnvName
	}
	environment := inspectEnvSummary{
		State:        opts.Selection.State,
		Source:       opts.Selection.Source,
		SelectedName: opts.Selection.SelectedName,
		SelectedPath: opts.Selection.SelectedPath,
		Environments: opts.Selection.Environments,
		DaemonEnv:    daemonEnv,
		Context:      opts.Context,
	}
	if opts.ContextMismatch {
		environment.ContextSwitchRequired = true
		environment.RunningName = projectContextName(opts.RunningPath)
		environment.RunningPath = opts.RunningPath
	}
	if opts.SetupRequired {
		environment = inspectEnvSummary{
			State:        environmentSelectionNone,
			Environments: []environmentChoice{},
		}
	}
	return inspectJSONData{
		SchemaVersion: inspectJSONSchemaVersion,
		Readiness:     readiness,
		Daemon: inspectDaemonSummary{
			Running:         opts.DaemonRunning,
			PID:             opts.PID,
			Version:         opts.Version,
			OnDisk:          opts.OnDisk,
			OnDiskPath:      opts.OnDiskPath,
			UpdateAvailable: opts.UpdateAvailable,
			Dashboard:       inspectDashboard(opts),
		},
		Environment:        environment,
		Resources:          services,
		Risks:              risks,
		RecommendedActions: actions,
	}
}

func configuredInspectServices(cfg *config.Config) []daemon.ResourceStatus {
	if cfg == nil {
		return nil
	}
	services := make([]daemon.ResourceStatus, 0, len(cfg.Containers)+len(cfg.Services))
	for name := range cfg.Containers {
		services = append(services, daemon.ResourceStatus{
			Name:  name,
			Kind:  daemon.ResourceKindContainer,
			State: "stopped",
		})
	}
	for name := range cfg.Services {
		services = append(services, daemon.ResourceStatus{
			Name:  name,
			Kind:  daemon.ResourceKindService,
			State: "stopped",
		})
	}
	return services
}

func inspectDashboard(opts inspectBuildOptions) string {
	if !opts.DaemonRunning || opts.ContextMismatch {
		return ""
	}
	return opts.Dashboard
}

func emptyInspectServiceSummary() inspectServiceSummary {
	return inspectServiceSummary{
		ByState:      map[string][]string{},
		Degraded:     []string{},
		Starting:     []string{},
		Stopped:      []string{},
		failureKinds: map[string]string{},
	}
}

func buildInspectServiceSummary(services []daemon.ResourceStatus) inspectServiceSummary {
	out := emptyInspectServiceSummary()
	out.Total = len(services)
	for i := range services {
		name := services[i].Name
		state := services[i].State
		out.ByState[state] = append(out.ByState[state], name)
		switch state {
		case "degraded":
			out.Degraded = append(out.Degraded, name)
			out.failureKinds[name] = services[i].FailureKind
		case "pending", "starting", "building", "stopping", "restarting":
			out.Starting = append(out.Starting, name)
		case "stopped":
			out.Stopped = append(out.Stopped, name)
		}
	}
	for state := range out.ByState {
		sort.Strings(out.ByState[state])
	}
	sort.Strings(out.Degraded)
	sort.Strings(out.Starting)
	sort.Strings(out.Stopped)
	return out
}

func buildInspectRisks(opts inspectBuildOptions, services inspectServiceSummary) []inspectRisk {
	risks := make([]inspectRisk, 0, 1+len(opts.ReadinessChecks))
	if risk, ok := inspectBlockingRisk(opts); ok {
		risks = append(risks, risk)
	} else {
		risks = append(risks, inspectServiceRisks(services)...)
	}
	for _, check := range opts.ReadinessChecks {
		message := check.Message
		if check.Hint != "" {
			message += ". " + check.Hint
		}
		risks = append(risks, inspectRisk{
			Code:     "dependency_readiness_ambiguous",
			Severity: "medium",
			Message:  message,
		})
	}
	return risks
}

func inspectBlockingRisk(opts inspectBuildOptions) (inspectRisk, bool) {
	if opts.SetupRequired {
		return inspectRisk{
			Code:     "setup_required",
			Severity: "critical",
			Message:  "Orbit has not been set up in this user context",
		}, true
	}
	if environmentSelectionBlocksConfig(opts.Selection, opts.ConfigPath) ||
		(opts.Selection.State == environmentSelectionNone && len(opts.Selection.Environments) > 0 && opts.ConfigErr != nil) {
		message := "an available environment must be selected"
		if environmentSelectionBlocksConfig(opts.Selection, opts.ConfigPath) {
			message = fmt.Sprintf("environment %q is no longer available", opts.Selection.SelectedName)
		}
		return inspectRisk{
			Code:     "environment_selection_required",
			Severity: "critical",
			Message:  message,
		}, true
	}
	if opts.ConfigErr != nil {
		return inspectRisk{
			Code:     "config_invalid",
			Severity: "critical",
			Message:  opts.ConfigErr.Error(),
		}, true
	}
	if opts.ContextMismatch {
		return inspectRisk{
			Code:     "project_context_inactive",
			Severity: "low",
			Message: fmt.Sprintf(
				"%s is running; orbit up switches to %s",
				projectContextName(opts.RunningPath),
				projectContextName(opts.ConfigPath),
			),
		}, true
	}
	if inspectEnvMismatch(opts) {
		return inspectRisk{
			Code:     "env_mismatch",
			Severity: "critical",
			Message:  fmt.Sprintf("selected env %q differs from daemon env %q", opts.ConfigEnvName, opts.DaemonEnv),
		}, true
	}
	if opts.UpdateAvailable {
		return inspectRisk{
			Code:     "orbit_update_pending",
			Severity: "critical",
			Message:  "an installed Orbit update requires a daemon restart",
		}, true
	}
	if !opts.DaemonRunning {
		return inspectRisk{
			Code:     "environment_stopped",
			Severity: "low",
			Message:  "the selected environment is not running",
		}, true
	}
	if opts.StatusErr != nil {
		return inspectRisk{
			Code:     "status_unavailable",
			Severity: "critical",
			Message:  opts.StatusErr.Error(),
		}, true
	}
	return inspectRisk{}, false
}

func inspectServiceRisks(services inspectServiceSummary) []inspectRisk {
	risks := make([]inspectRisk, 0, len(services.Degraded)+len(services.Starting)+len(services.Stopped))
	for _, name := range services.Degraded {
		risks = append(risks, inspectRisk{
			Code:     "resource_degraded",
			Severity: "high",
			Message:  name + " is degraded",
			Resource: name,
		})
	}
	for _, name := range services.Starting {
		risks = append(risks, inspectRisk{
			Code:     "resource_converging",
			Severity: "medium",
			Message:  name + " is not healthy yet",
			Resource: name,
		})
	}
	for _, name := range services.Stopped {
		risks = append(risks, inspectRisk{
			Code:     "resource_stopped",
			Severity: "low",
			Message:  name + " is stopped",
			Resource: name,
		})
	}
	return risks
}

func deriveInspectReadiness(opts inspectBuildOptions, services inspectServiceSummary) inspectReadiness {
	if opts.SetupRequired {
		return inspectReadiness{State: inspectReadinessSetupRequired, Blocked: true, Summary: "Orbit setup is required before startup"}
	}
	if environmentSelectionBlocksConfig(opts.Selection, opts.ConfigPath) ||
		(opts.Selection.State == environmentSelectionNone && len(opts.Selection.Environments) > 0 && opts.ConfigErr != nil) {
		return inspectReadiness{State: inspectReadinessSelectionRequired, Blocked: true, Summary: "an available environment must be selected"}
	}
	if opts.ConfigErr != nil {
		return inspectReadiness{State: inspectReadinessConfigInvalid, Blocked: true, Summary: "selected config cannot be loaded"}
	}
	if opts.ContextMismatch {
		return inspectReadiness{
			State:   inspectReadinessNeedsDaemon,
			Blocked: true,
			Summary: "another project is running; orbit up switches to this project",
		}
	}
	if inspectEnvMismatch(opts) {
		return inspectReadiness{State: inspectReadinessNeedsDaemon, Blocked: true, Summary: "daemon is running with a different env"}
	}
	if opts.UpdateAvailable {
		return inspectReadiness{State: inspectReadinessUpdateRequired, Blocked: true, Summary: "restart Orbit to run the installed version"}
	}
	if !opts.DaemonRunning {
		return inspectReadiness{State: inspectReadinessStopped, Blocked: true, Summary: "the selected environment is not running"}
	}
	if opts.StatusErr != nil {
		return inspectReadiness{State: inspectReadinessConverging, Summary: "daemon is running, but resource status is unavailable"}
	}
	if len(services.Degraded) > 0 {
		return inspectReadiness{State: inspectReadinessDegraded, Summary: "daemon is running, but one or more resources are degraded"}
	}
	if len(services.Starting) > 0 {
		return inspectReadiness{State: inspectReadinessConverging, Summary: "daemon is running and resources are still converging"}
	}
	if len(services.Stopped) > 0 {
		return inspectReadiness{State: inspectReadinessPartial, Summary: "daemon is running, but one or more resources are stopped"}
	}
	return inspectReadiness{State: inspectReadinessReady, Summary: "daemon is running and no resource risks were detected"}
}

func inspectEnvMismatch(opts inspectBuildOptions) bool {
	if opts.ConfigMatchesDaemon {
		return false
	}
	return opts.DaemonRunning && opts.ConfigEnvName != "" && opts.DaemonEnv != "" && opts.ConfigEnvName != opts.DaemonEnv
}

func inspectRecommendedActions(
	readiness inspectReadiness,
	risks []inspectRisk,
	services inspectServiceSummary,
	configErr error,
	selectedEnv string,
	selection environmentSelection,
) []cli.JSONAction {
	actions := []cli.JSONAction{}
	switch readiness.State {
	case inspectReadinessSetupRequired:
		actions = append(actions, cli.JSONAction{
			Command:     "orbit init --yes --json",
			Reason:      "Set up a workspace and select an environment before starting Orbit.",
			Destructive: false,
		})
	case inspectReadinessSelectionRequired:
		actions = append(actions, environmentSelectionActions(selection)...)
	case inspectReadinessUpdateRequired:
		actions = append(actions, cli.JSONAction{
			Command:     orbitRestartCommand(true),
			Reason:      "Restart Orbit to run the installed version.",
			Destructive: false,
		})
	case inspectReadinessConfigInvalid:
		var mismatch *config.SchemaVersionMismatchError
		if !errors.As(configErr, &mismatch) {
			break
		}
		switch mismatch.Kind {
		case config.SchemaVersionOlder:
			if cli.IsManagedEnvironmentPath(mismatch.Path) {
				actions = append(actions, cli.JSONAction{
					Command:     "orbit source sync --json",
					Reason:      "Refresh the shared environment file to the supported schema.",
					Destructive: false,
				})
			}
		case config.SchemaVersionNewer:
			actions = append(actions, cli.JSONAction{
				Command:     "orbit update --json",
				Reason:      "Update Orbit to support this environment schema.",
				Destructive: false,
			})
		}
	case inspectReadinessNeedsDaemon:
		if inspectHasRisk(risks, "env_mismatch") {
			command := "orbit daemon restart --json"
			if selectedEnv != "" {
				command = "orbit switch " + shellquote.Quote(selectedEnv) + " --json"
			}
			actions = append(actions, cli.JSONAction{
				Command:     command,
				Reason:      "Apply the selected environment through Orbit's safe switch workflow.",
				Destructive: false,
			})
		} else {
			actions = append(actions, cli.JSONAction{
				Command:     "orbit up --json",
				Reason:      "Start the selected environment.",
				Destructive: false,
			})
		}
	case inspectReadinessStopped:
		actions = append(actions, cli.JSONAction{
			Command:     "orbit up --json",
			Reason:      "Start the selected environment.",
			Destructive: false,
		})
	case inspectReadinessDegraded:
		for _, name := range services.Degraded {
			reason := "Review the exit output for " + name + " before retrying it."
			if services.failureKinds[name] == string(engine.FailureKindHealth) {
				reason = "Review application output that may explain the failed health probe; the process is still running."
			}
			actions = append(actions, cli.JSONAction{
				Command:     "orbit logs " + name + " --json",
				Reason:      reason,
				Destructive: false,
			})
		}
	case inspectReadinessConverging:
		actions = append(actions, cli.StatusAction())
	case inspectReadinessPartial:
		actions = append(actions, cli.JSONAction{
			Command:     "orbit up --json",
			Reason:      "Start stopped services and then inspect again.",
			Destructive: false,
		})
	}
	if inspectHasRisk(risks, "status_unavailable") {
		actions = append(actions, cli.DoctorAction())
	}
	return cli.MergeActions(nil, actions)
}

func inspectHasRisk(risks []inspectRisk, code string) bool {
	for _, risk := range risks {
		if risk.Code == code {
			return true
		}
	}
	return false
}

func printInspectHuman(data inspectJSONData) {
	fmt.Printf("Readiness: %s\n", data.Readiness.State)
	if data.Readiness.Summary != "" {
		fmt.Println(data.Readiness.Summary)
	}
	if data.Daemon.Running {
		fmt.Print("Daemon: running")
		if data.Daemon.PID != 0 {
			fmt.Printf(" (pid %d)", data.Daemon.PID)
		}
		fmt.Println()
	} else if data.Readiness.State == inspectReadinessStopped {
		fmt.Println("Environment: not running")
	} else {
		fmt.Println("Daemon: not running")
	}
	if len(data.Risks) > 0 {
		fmt.Println("Risks:")
		for _, risk := range data.Risks {
			fmt.Printf("  %s: %s\n", risk.Severity, risk.Message)
		}
	}
	if len(data.RecommendedActions) > 0 {
		if data.Readiness.State == inspectReadinessSelectionRequired && len(data.Environment.Environments) > 0 {
			fmt.Println("Available environments:")
			for _, environment := range data.Environment.Environments {
				fmt.Printf("  %s\n", environmentSwitchCommand(environment.Name, false))
			}
			return
		}
		next := strings.TrimSuffix(data.RecommendedActions[0].Command, " --json")
		if data.Readiness.State == inspectReadinessSetupRequired {
			next = "orbit init"
		}
		fmt.Printf("Next: %s\n", next)
	}
}
