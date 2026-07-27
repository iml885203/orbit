package app

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show service status",
		RunE:  runStatus,
	}
}

func runStatus(_ *cobra.Command, _ []string) error {
	cfg, cfgErr := config.Load(configFile)

	client := daemon.NewClient(daemon.DefaultSocketPath())
	daemonRunning := client.Health() == nil

	dstatus := daemonStatus{Running: daemonRunning}
	running := make(map[string]daemon.ServiceStatus)
	if daemonRunning {
		if status, err := client.Status(); err == nil {
			if mismatch := daemon.CheckConfigMatch(configFile, status.ConfigPath); mismatch != nil {
				return mismatch
			}
			for i := range status.Services {
				running[status.Services[i].Name] = status.Services[i]
			}
			dstatus.ConfigStale = status.ConfigStale
			dstatus.ConfigStaleReason = status.ConfigStaleReason
		}
		if v, err := client.Version(); err == nil {
			dstatus.Version = v.Running
			dstatus.OnDisk = v.OnDisk
			dstatus.OnDiskPath = v.OnDiskPath
			dstatus.UpdateAvailable = v.UpdateAvailable
		}
	}

	if cfgErr != nil && !daemonRunning {
		if cli.JSONOutput {
			return writeStatusJSON(os.Stdout, commandString(), cfg, running, dstatus)
		}
		fmt.Println("No daemon running. Start it with 'orbit up'.")
		return nil
	}

	if cli.JSONOutput {
		return writeStatusJSON(os.Stdout, commandString(), cfg, running, dstatus)
	}

	printDaemonHeader(dstatus)

	// Track state for tips
	var stoppedInfra bool
	var stoppedServices []string
	var openableServices []string

	// Containers
	_, _ = cli.Bold.Println("CONTAINERS")
	if cfg != nil {
		names := sortedKeys(cfg.Containers)
		for _, name := range names {
			c := cfg.Containers[name]
			if svc, ok := running[name]; ok {
				printContainerLine(name, svc)
				printStatusDetail(svc, running)
				if svc.State == "stopped" {
					stoppedInfra = true
				}
			} else {
				ports := configPorts(c.Ports)
				fmt.Printf("  %s %-20s  %-10s %s\n", cli.Faint.Sprint("○"), name, cli.Faint.Sprint("stopped"), cli.Faint.Sprint(ports))
				stoppedInfra = true
			}
		}
	}

	// Services
	fmt.Println()
	_, _ = cli.Bold.Println("SERVICES")
	if cfg != nil {
		names := sortedKeys(cfg.Services)
		for _, name := range names {
			s := cfg.Services[name]
			if svc, ok := running[name]; ok {
				printServiceLine(name, s, svc)
				printStatusDetail(svc, running)
				if svc.State == "stopped" {
					stoppedServices = append(stoppedServices, name)
				}
				if svc.URL != "" && svc.State == "healthy" {
					openableServices = append(openableServices, name)
				}
			} else {
				info := ""
				if s.URL != "" {
					info = cli.Faint.Sprint(s.URL)
				} else {
					info = cli.Faint.Sprint(configPorts(s.Ports))
				}
				fmt.Printf("  %s %-20s  %-10s %s\n", cli.Faint.Sprint("○"), name, cli.Faint.Sprint("stopped"), info)
				stoppedServices = append(stoppedServices, name)
			}
		}
	}

	// Context-aware tips
	tips := buildTips(cfg, daemonRunning, stoppedInfra, stoppedServices, statusRecoveryTargets(running), openableServices)
	if len(tips) > 0 {
		fmt.Println()
		for _, tip := range tips {
			_, _ = cli.Faint.Printf("  %s\n", tip)
		}
	}

	return nil
}

func printContainerLine(name string, svc daemon.ServiceStatus) {
	icon := cli.StateIcon(svc.State)
	ports := formatPorts(svc.Ports)
	timing := formatTiming(svc)
	fmt.Printf("  %s %-20s  %-10s %-30s %s\n", icon, name, cli.ColorState(svc.State), ports, cli.Faint.Sprint(timing))
}

func printServiceLine(name string, _ *config.Service, svc daemon.ServiceStatus) {
	icon := cli.StateIcon(svc.State)
	extra := ""
	if svc.RestartCount > 0 {
		extra = fmt.Sprintf(" (restarts: %d)", svc.RestartCount)
	}
	info := ""
	if svc.URL != "" {
		info = cli.Faint.Sprint(svc.URL)
	} else {
		info = formatPorts(svc.Ports)
	}
	timing := formatTiming(svc)
	fmt.Printf("  %s %-20s  %-10s %s%s %s\n", icon, name, cli.ColorState(svc.State), info, extra, cli.Faint.Sprint(timing))
}

func printStatusDetail(svc daemon.ServiceStatus, running map[string]daemon.ServiceStatus) {
	detail := statusDetail(svc, running)
	if detail != "" {
		fmt.Printf("    %s %s\n", cli.Faint.Sprint("↳"), detail)
	}
}

func statusDetail(svc daemon.ServiceStatus, running map[string]daemon.ServiceStatus) string {
	switch svc.State {
	case "degraded":
		return serviceFailureReason(svc)
	case "pending":
		if blocker := statusDependencyBlocker(svc, running); blocker != nil {
			detail := "blocked by " + blocker.Name
			if reason := serviceFailureReason(*blocker); reason != "" {
				detail += " — " + reason
			}
			return detail
		}
	}
	return ""
}

func serviceFailureReason(svc daemon.ServiceStatus) string {
	if svc.StateReason != "" {
		return svc.StateReason
	}
	if svc.HealthProgress != nil {
		return svc.HealthProgress.LastErr
	}
	return ""
}

func formatTiming(svc daemon.ServiceStatus) string {
	parts := make([]string, 0, 2)
	if svc.StartupTime != "" {
		parts = append(parts, "started in "+svc.StartupTime)
	}
	if svc.Uptime != "" {
		parts = append(parts, "up "+svc.Uptime)
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func buildTips(cfg *config.Config, daemonRunning, stoppedInfra bool, stoppedServices, degradedNames, openableServices []string) []string {
	var tips []string

	// Scenario 1: nothing running
	if !daemonRunning {
		tips = append(tips, "orbit up                  start environment")
		return tips
	}

	// Scenario 5: degraded — most urgent
	if len(degradedNames) > 0 {
		for _, name := range degradedNames {
			tips = append(tips,
				fmt.Sprintf("orbit logs %-14s  check logs", name),
				fmt.Sprintf("orbit restart %-11s  restart service", name),
			)
		}
		return tips
	}

	// Scenario 2: infra stopped
	if stoppedInfra {
		tips = append(tips, "orbit up --infra          start infrastructure")
		return tips
	}

	// Scenario 3: some services stopped
	if len(stoppedServices) > 0 {
		if cfg != nil && len(stoppedServices) == len(cfg.Services) {
			tips = append(tips, "orbit up                  start all services")
		} else {
			for _, name := range stoppedServices {
				tips = append(tips, fmt.Sprintf("orbit up %-16s  start service", name))
			}
		}
	}

	// Scenario 4: healthy services with URLs
	for _, name := range openableServices {
		tips = append(tips, fmt.Sprintf("orbit open %-14s  open in browser", name))
	}

	return tips
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func configPorts(ports map[string]config.PortDef) string {
	if len(ports) == 0 {
		return ""
	}
	labels := make([]string, 0, len(ports))
	for label := range ports {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, portURL(label, ports[label].Host))
	}
	return strings.Join(parts, " ")
}

type statusJSONData struct {
	Daemon   daemonStatus  `json:"daemon"`
	Services []jsonService `json:"services"`
}

type jsonService struct {
	Name                string         `json:"name"`
	Kind                string         `json:"kind"`
	State               string         `json:"state"`
	StateReason         string         `json:"state_reason,omitempty"`
	PendingDependencies []string       `json:"pending_dependencies,omitempty"`
	BlockedBy           string         `json:"blocked_by,omitempty"`
	URL                 string         `json:"url,omitempty"`
	Ports               map[string]int `json:"ports,omitempty"`
	StartupTime         string         `json:"startup_time,omitempty"`
	Uptime              string         `json:"uptime,omitempty"`
}

type daemonStatus struct {
	Running           bool   `json:"running"`
	Version           string `json:"version,omitempty"`
	OnDisk            string `json:"on_disk,omitempty"`
	OnDiskPath        string `json:"on_disk_path,omitempty"`
	UpdateAvailable   bool   `json:"update_available"`
	ConfigStale       bool   `json:"config_stale,omitempty"`
	ConfigStaleReason string `json:"config_stale_reason,omitempty"`
}

func writeStatusJSON(w io.Writer, command string, cfg *config.Config, running map[string]daemon.ServiceStatus, dstatus daemonStatus) error {
	services := make([]jsonService, 0)

	if cfg != nil {
		for name, c := range cfg.Containers {
			svc := jsonService{Name: name, Kind: "container", State: "stopped"}
			ports := make(map[string]int, len(c.Ports))
			for label, p := range c.Ports {
				ports[label] = p.Host
			}
			svc.Ports = ports
			if r, ok := running[name]; ok {
				applyRuntimeStatus(&svc, r, running)
			}
			services = append(services, svc)
		}
		for name, s := range cfg.Services {
			svc := jsonService{Name: name, Kind: "service", State: "stopped", URL: s.URL}
			ports := make(map[string]int, len(s.Ports))
			for label, p := range s.Ports {
				ports[label] = p.Host
			}
			svc.Ports = ports
			if r, ok := running[name]; ok {
				applyRuntimeStatus(&svc, r, running)
			}
			services = append(services, svc)
		}
	}

	sort.Slice(services, func(i, j int) bool {
		if services[i].Kind != services[j].Kind {
			return services[i].Kind < services[j].Kind
		}
		return services[i].Name < services[j].Name
	})

	var actions []cli.JSONAction
	if !dstatus.Running {
		actions = append(actions, cli.JSONAction{
			Command: "orbit up --json",
			Reason:  "Start the selected environment and its daemon.",
		})
	}
	if dstatus.ConfigStale {
		actions = append(actions, cli.JSONAction{
			Command: "orbit daemon restart",
			Reason:  "Apply the changed config: " + dstatus.ConfigStaleReason + ".",
		})
	}
	actions = cli.MergeActions(actions, statusRecoveryActions(running))
	return cli.WriteJSONSuccess(w, command, statusJSONData{
		Daemon:   dstatus,
		Services: services,
	}, actions)
}

func applyRuntimeStatus(target *jsonService, source daemon.ServiceStatus, running map[string]daemon.ServiceStatus) {
	target.State = source.State
	if source.State == "degraded" {
		target.StateReason = serviceFailureReason(source)
	}
	target.PendingDependencies = append([]string{}, source.PendingDependencies...)
	target.StartupTime = source.StartupTime
	target.Uptime = source.Uptime
	if blocker := statusDependencyBlocker(source, running); blocker != nil {
		target.BlockedBy = blocker.Name
	}
}

func statusDependencyBlocker(service daemon.ServiceStatus, running map[string]daemon.ServiceStatus) *daemon.ServiceStatus {
	if service.State != "pending" || len(service.PendingDependencies) == 0 {
		return nil
	}
	status := &daemon.StatusResponse{Services: make([]daemon.ServiceStatus, 0, len(running))}
	for _, candidate := range running {
		status.Services = append(status.Services, candidate)
	}
	return terminalDependencyBlocker(status, service.PendingDependencies)
}

func statusRecoveryTargets(running map[string]daemon.ServiceStatus) []string {
	targets := make(map[string]bool)
	for _, service := range running {
		if service.State == "degraded" && (service.HealthProgress == nil || !service.HealthProgress.Recovering) {
			targets[service.Name] = true
		}
		if blocker := statusDependencyBlocker(service, running); blocker != nil {
			targets[blocker.Name] = true
		}
	}
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func statusRecoveryActions(running map[string]daemon.ServiceStatus) []cli.JSONAction {
	var actions []cli.JSONAction
	for _, name := range statusRecoveryTargets(running) {
		service := running[name]
		if service.State == "stopped" {
			actions = append(actions, cli.JSONAction{
				Command: "orbit up " + name + " --json",
				Reason:  "Start " + name + ", which is blocking dependent services.",
			})
			continue
		}
		actions = append(actions,
			cli.JSONAction{
				Command: "orbit logs " + name + " --json",
				Reason:  "Inspect recent logs for " + name + ".",
			},
			cli.JSONAction{
				Command: "orbit restart " + name + " --json",
				Reason:  "Retry " + name + " after fixing the reported cause.",
			},
		)
	}
	return actions
}

func printDaemonHeader(s daemonStatus) {
	_, _ = cli.Bold.Println("DAEMON")
	if !s.Running {
		fmt.Printf("  %s %s\n\n", cli.Faint.Sprint("○"), cli.Faint.Sprint("not running"))
		return
	}
	ver := s.Version
	if ver == "" {
		ver = cli.Faint.Sprint("version unknown")
	}
	fmt.Printf("  %s %s\n", cli.ColorState("healthy"), ver)
	if s.UpdateAvailable && s.OnDisk != "" {
		location := s.OnDisk
		if s.OnDiskPath != "" {
			location = fmt.Sprintf("%s at %s", s.OnDisk, s.OnDiskPath)
		}
		fmt.Printf("  %s newer orbit %s — orbit daemon restart\n",
			cli.Faint.Sprint("⚠"), location)
	}
	if s.ConfigStale {
		fmt.Printf("  %s %s — orbit daemon restart to apply\n",
			cli.Faint.Sprint("⚠"), s.ConfigStaleReason)
	}
	fmt.Println()
}

func formatPorts(ports map[string]int) string {
	if len(ports) == 0 {
		return ""
	}
	labels := make([]string, 0, len(ports))
	for label := range ports {
		labels = append(labels, label)
	}
	sort.Strings(labels)

	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, portURL(label, ports[label]))
	}
	return strings.Join(parts, " ")
}

func portURL(label string, port int) string {
	switch label {
	case "https":
		return fmt.Sprintf("https://localhost:%d", port)
	case "http", "dev", "ui":
		return fmt.Sprintf("http://localhost:%d", port)
	default:
		return fmt.Sprintf(":%d", port)
	}
}
