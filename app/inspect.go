package app

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/spf13/cobra"
)

const inspectJSONSchemaVersion = "orbit.inspect.v1"

const (
	inspectReadinessSetupRequired = "setup_required"
	inspectReadinessConfigInvalid = "config_invalid"
	inspectReadinessNeedsDaemon   = "needs_daemon"
	inspectReadinessStopped       = "stopped"
	inspectReadinessDegraded      = "degraded"
	inspectReadinessConverging    = "converging"
	inspectReadinessPartial       = "partial"
	inspectReadinessReady         = "ready"
)

type inspectJSONData struct {
	SchemaVersion      string                `json:"schema_version"`
	Readiness          inspectReadiness      `json:"readiness"`
	Daemon             inspectDaemonSummary  `json:"daemon"`
	Env                inspectEnvSummary     `json:"env"`
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
	Name        string `json:"name,omitempty"`
	ConfigPath  string `json:"config_path,omitempty"`
	PreviewOnly bool   `json:"preview_only,omitempty"`
	DaemonEnv   string `json:"daemon_env,omitempty"`
}

type inspectServiceSummary struct {
	Total    int                 `json:"total"`
	ByState  map[string][]string `json:"by_state"`
	Degraded []string            `json:"degraded"`
	Starting []string            `json:"starting"`
	Stopped  []string            `json:"stopped"`
}

type inspectRisk struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Resource string `json:"resource,omitempty"`
}

type inspectBuildOptions struct {
	Command         string
	ConfigPath      string
	ConfigErr       error
	SetupRequired   bool
	ConfigEnvName   string
	PreviewOnly     bool
	DaemonEnv       string
	DaemonRunning   bool
	PID             int
	Version         string
	OnDisk          string
	OnDiskPath      string
	UpdateAvailable bool
	Dashboard       string
	Status          *daemon.StatusResponse
	StatusErr       error
	Configured      []daemon.ResourceStatus
}

func inspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "inspect",
		Short:  "Show an agent-ready Orbit state snapshot",
		Hidden: true,
		RunE:   runInspect,
	}
}

func runInspect(_ *cobra.Command, _ []string) error {
	cfg, cfgErr := config.Load(configFile)
	client := daemon.NewClient(daemon.DefaultSocketPath())
	daemonRunning := client.Health() == nil
	setupRequired := cfgErr != nil && !daemonRunning && !configFileExists(configFile)
	pid, alive := daemon.IsDaemonRunning()
	if daemonRunning {
		pid = alivePID(pid, alive)
	}

	opts := inspectBuildOptions{
		Command:       commandString(),
		ConfigPath:    configFile,
		ConfigErr:     cfgErr,
		SetupRequired: setupRequired,
		ConfigEnvName: daemonsrv.EnvShortName(configFile),
		DaemonRunning: daemonRunning,
		PID:           pid,
		Dashboard:     fmt.Sprintf("http://localhost:%d", daemon.DashboardPort()),
	}
	if cfg != nil {
		opts.PreviewOnly = cfg.PreviewOnly
		opts.Configured = configuredInspectServices(cfg)
	}
	if daemonRunning {
		if status, err := client.Status(); err == nil {
			opts.Status = status
		} else {
			opts.StatusErr = err
		}
		if version, err := client.Version(); err == nil {
			opts.Version = version.Running
			opts.OnDisk = version.OnDisk
			opts.OnDiskPath = version.OnDiskPath
			opts.UpdateAvailable = version.UpdateAvailable
		}
		if envs, err := client.Envs(); err == nil {
			opts.DaemonEnv = daemonsrv.EnvShortName(envs.Current)
		}
	}

	data := buildInspectData(opts)
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), data, data.RecommendedActions)
	}
	printInspectHuman(data)
	return nil
}

func alivePID(pid int, alive bool) int {
	if !alive {
		return 0
	}
	return pid
}

func buildInspectData(opts inspectBuildOptions) inspectJSONData {
	services := emptyInspectServiceSummary()
	if opts.Status != nil {
		services = buildInspectServiceSummary(opts.Status.Resources)
	} else if len(opts.Configured) > 0 {
		services = buildInspectServiceSummary(opts.Configured)
	}
	risks := buildInspectRisks(opts, services)
	readiness := deriveInspectReadiness(opts, services)
	actions := inspectRecommendedActions(readiness, risks, services, opts.Command, opts.ConfigEnvName)
	env := inspectEnvSummary{
		Name:        opts.ConfigEnvName,
		ConfigPath:  opts.ConfigPath,
		PreviewOnly: opts.PreviewOnly,
		DaemonEnv:   opts.DaemonEnv,
	}
	if opts.SetupRequired {
		env = inspectEnvSummary{}
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
		Env:                env,
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
	if !opts.DaemonRunning {
		return ""
	}
	return opts.Dashboard
}

func emptyInspectServiceSummary() inspectServiceSummary {
	return inspectServiceSummary{
		ByState:  map[string][]string{},
		Degraded: []string{},
		Starting: []string{},
		Stopped:  []string{},
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
	if risk, ok := inspectBlockingRisk(opts); ok {
		return []inspectRisk{risk}
	}
	return inspectServiceRisks(services)
}

func inspectBlockingRisk(opts inspectBuildOptions) (inspectRisk, bool) {
	if opts.SetupRequired {
		return inspectRisk{
			Code:     "setup_required",
			Severity: "critical",
			Message:  "Orbit has not been set up in this user context",
		}, true
	}
	if opts.ConfigErr != nil {
		return inspectRisk{
			Code:     "config_invalid",
			Severity: "critical",
			Message:  opts.ConfigErr.Error(),
		}, true
	}
	if inspectEnvMismatch(opts) {
		return inspectRisk{
			Code:     "env_mismatch",
			Severity: "critical",
			Message:  fmt.Sprintf("selected env %q differs from daemon env %q", opts.ConfigEnvName, opts.DaemonEnv),
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
	if opts.ConfigErr != nil {
		return inspectReadiness{State: inspectReadinessConfigInvalid, Blocked: true, Summary: "selected config cannot be loaded"}
	}
	if inspectEnvMismatch(opts) {
		return inspectReadiness{State: inspectReadinessNeedsDaemon, Blocked: true, Summary: "daemon is running with a different env"}
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
	return opts.DaemonRunning && opts.ConfigEnvName != "" && opts.DaemonEnv != "" && opts.ConfigEnvName != opts.DaemonEnv
}

func inspectRecommendedActions(
	readiness inspectReadiness,
	risks []inspectRisk,
	services inspectServiceSummary,
	retryCommand string,
	selectedEnv string,
) []cli.JSONAction {
	actions := []cli.JSONAction{}
	switch readiness.State {
	case inspectReadinessSetupRequired:
		actions = append(actions, cli.JSONAction{
			Command:     "orbit init --yes --json",
			Reason:      "Set up a workspace and select an environment before starting Orbit.",
			Destructive: false,
		})
	case inspectReadinessConfigInvalid:
		command := "orbit inspect --json"
		if retryCommand != "" {
			command = retryCommand
		}
		actions = append(actions, cli.JSONAction{
			Command:     command,
			Reason:      "Retry inspection after fixing the reported environment file.",
			Destructive: false,
		})
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
			actions = append(actions,
				cli.JSONAction{
					Command:     "orbit logs " + name + " --json",
					Reason:      "Inspect logs for degraded service " + name + ".",
					Destructive: false,
				},
				cli.JSONAction{
					Command:     "orbit restart " + name + " --json",
					Reason:      "Retry " + name + " after fixing the reported cause.",
					Destructive: false,
				},
			)
		}
	case inspectReadinessConverging:
		actions = append(actions, cli.StatusAction())
	case inspectReadinessPartial:
		actions = append(actions, cli.JSONAction{
			Command:     "orbit up --json",
			Reason:      "Start stopped services and then inspect again.",
			Destructive: false,
		})
	default:
		actions = append(actions, cli.StatusAction())
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
	if data.Env.ConfigPath != "" {
		fmt.Printf("Config: %s\n", data.Env.ConfigPath)
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
		next := strings.TrimSuffix(data.RecommendedActions[0].Command, " --json")
		if data.Readiness.State == inspectReadinessSetupRequired {
			next = "orbit init"
		}
		fmt.Printf("Next: %s\n", next)
	}
}
