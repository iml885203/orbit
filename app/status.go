package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	selection := readEnvironmentSelection()
	cfg, cfgErr := config.Load(configFile)

	client := daemon.NewClient(daemon.DefaultSocketPath())
	daemonRunning := client.Health() == nil

	dstatus := daemonStatus{Running: daemonRunning}
	running := make(map[string]daemon.ResourceStatus)
	if daemonRunning {
		if status, err := client.Status(); err == nil {
			if mismatch := daemon.CheckConfigMatch(configFile, status.ConfigPath); mismatch != nil {
				return mismatch
			}
			for i := range status.Resources {
				running[status.Resources[i].Name] = status.Resources[i]
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

	setup := statusSetupState{Selection: selection}
	if environmentSelectionBlocksConfig(selection, configFile) {
		setup.SelectionRequired = true
		setup.Message = environmentSelectionMessage(selection)
		if cli.JSONOutput {
			return writeStatusJSON(os.Stdout, commandString(), nil, running, dstatus, setup)
		}
		printEnvironmentSelectionRecovery(selection)
		if daemonRunning {
			fmt.Println()
			printDaemonHeader(dstatus)
			_, _, _ = printRunningSnapshot(running)
		}
		return nil
	}
	if cfgErr != nil && configFileExists(configFile) {
		err := cli.NewInvalidEnvironmentError(
			fmt.Sprintf("active environment %s is invalid: %v", filepath.Base(configFile), cfgErr),
		)
		return cli.WithJSONActions(err, []cli.JSONAction{{
			Command:     commandString(),
			Reason:      "Retry status after fixing the reported environment file.",
			Destructive: false,
		}})
	}
	if cfgErr != nil && daemonRunning {
		err := cli.NewInvalidEnvironmentError(
			fmt.Sprintf("active environment is unavailable while the daemon is still running: %v", cfgErr),
		)
		return cli.WithJSONActions(err, []cli.JSONAction{{
			Command:     "orbit init --yes --json",
			Reason:      "Restore setup and select an environment without stopping the running daemon.",
			Destructive: false,
		}})
	}
	if cfgErr != nil && !daemonRunning {
		setup = statusSetupState{
			Required:  true,
			Message:   "No usable environment is selected. Run Orbit setup first.",
			Selection: selection,
		}
		if cli.JSONOutput {
			return writeStatusJSON(os.Stdout, commandString(), cfg, running, dstatus, setup)
		}
		fmt.Println("Orbit is not set up yet.")
		fmt.Println("  Next: orbit init")
		return nil
	}

	if cli.JSONOutput {
		return writeStatusJSON(os.Stdout, commandString(), cfg, running, dstatus, setup)
	}

	printDaemonHeader(dstatus)

	// Track state for tips
	var stoppedInfra bool
	var stoppedServices []string
	var openableServices []string

	if dstatus.ConfigStale || setup.SelectionRequired {
		stoppedInfra, stoppedServices, openableServices = printRunningSnapshot(running)
	} else {
		// Containers
		_, _ = cli.Bold.Println("CONTAINERS")
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

		// Services
		fmt.Println()
		_, _ = cli.Bold.Println("SERVICES")
		names = sortedKeys(cfg.Services)
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
	var tips []string
	if dstatus.ConfigStale {
		tips = []string{"orbit daemon restart      apply environment changes"}
	} else if dstatus.UpdateAvailable {
		tips = []string{"orbit daemon restart      use the installed Orbit version"}
	} else {
		tips = buildTips(cfg, daemonRunning, stoppedInfra, stoppedServices, statusRecoveryTargets(running), openableServices)
	}
	if len(tips) > 0 {
		fmt.Println()
		for _, tip := range tips {
			_, _ = cli.Faint.Printf("  %s\n", tip)
		}
	}

	return nil
}

func printRunningSnapshot(running map[string]daemon.ResourceStatus) (bool, []string, []string) {
	var stoppedInfra bool
	var stoppedServices []string
	var openableServices []string

	_, _ = cli.Bold.Println("CONTAINERS")
	for _, name := range sortedResourceNames(running, daemon.ResourceKindContainer) {
		svc := running[name]
		printContainerLine(name, svc)
		printStatusDetail(svc, running)
		if svc.State == "stopped" {
			stoppedInfra = true
		}
	}

	fmt.Println()
	_, _ = cli.Bold.Println("SERVICES")
	for _, name := range sortedResourceNames(running, daemon.ResourceKindService) {
		svc := running[name]
		printServiceLine(name, nil, svc)
		printStatusDetail(svc, running)
		if svc.State == "stopped" {
			stoppedServices = append(stoppedServices, name)
		}
		if svc.URL != "" && svc.State == "healthy" {
			openableServices = append(openableServices, name)
		}
	}
	return stoppedInfra, stoppedServices, openableServices
}

func sortedResourceNames(running map[string]daemon.ResourceStatus, kind daemon.ResourceKind) []string {
	names := make([]string, 0, len(running))
	for name, resource := range running {
		if resource.Kind == kind {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func configFileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func printContainerLine(name string, svc daemon.ResourceStatus) {
	icon := cli.StateIcon(svc.State)
	ports := formatPorts(svc.Ports)
	timing := formatTiming(svc)
	fmt.Printf("  %s %-20s  %-10s %-30s %s\n", icon, name, cli.ColorState(svc.State), ports, cli.Faint.Sprint(timing))
}

func printServiceLine(name string, _ *config.Service, svc daemon.ResourceStatus) {
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

func printStatusDetail(svc daemon.ResourceStatus, running map[string]daemon.ResourceStatus) {
	detail := statusDetail(svc, running)
	if detail != "" {
		fmt.Printf("    %s %s\n", cli.Faint.Sprint("↳"), detail)
	}
}

func statusDetail(svc daemon.ResourceStatus, running map[string]daemon.ResourceStatus) string {
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

func serviceFailureReason(svc daemon.ResourceStatus) string {
	if svc.StateReason != "" {
		return svc.StateReason
	}
	if svc.HealthProgress != nil {
		return svc.HealthProgress.LastErr
	}
	return ""
}

func formatTiming(svc daemon.ResourceStatus) string {
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
	SetupRequired     bool                 `json:"setup_required"`
	SelectionRequired bool                 `json:"selection_required,omitempty"`
	SetupMessage      string               `json:"setup_message,omitempty"`
	SelectionMessage  string               `json:"selection_message,omitempty"`
	Environment       environmentSelection `json:"environment"`
	Daemon            daemonStatus         `json:"daemon"`
	Resources         []jsonService        `json:"resources"`
}

type statusSetupState struct {
	Required          bool
	SelectionRequired bool
	Message           string
	Selection         environmentSelection
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

func writeStatusJSON(
	w io.Writer,
	command string,
	cfg *config.Config,
	running map[string]daemon.ResourceStatus,
	dstatus daemonStatus,
	setup statusSetupState,
) error {
	if setup.Selection.Environments == nil {
		setup.Selection.Environments = []environmentChoice{}
	}
	resources := make([]jsonService, 0)

	if dstatus.ConfigStale || setup.SelectionRequired {
		for name, resource := range running {
			svc := jsonService{
				Name:  name,
				Kind:  string(resource.Kind),
				State: resource.State,
				URL:   resource.URL,
				Ports: resource.Ports,
			}
			applyRuntimeStatus(&svc, resource, running)
			resources = append(resources, svc)
		}
	} else if cfg != nil {
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
			resources = append(resources, svc)
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
			resources = append(resources, svc)
		}
	}

	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Kind != resources[j].Kind {
			return resources[i].Kind < resources[j].Kind
		}
		return resources[i].Name < resources[j].Name
	})

	var actions []cli.JSONAction
	if setup.SelectionRequired {
		actions = environmentSelectionActions(setup.Selection)
	} else if dstatus.ConfigStale {
		actions = append(actions, cli.JSONAction{
			Command: "orbit daemon restart --json",
			Reason:  "Apply the selected environment changes before running resource commands.",
		})
	} else if dstatus.UpdateAvailable {
		actions = append(actions, cli.JSONAction{
			Command: "orbit daemon restart --json",
			Reason:  "Run the installed Orbit version before starting resources.",
		})
	} else if setup.Required {
		actions = append(actions, cli.JSONAction{
			Command: "orbit init --yes --json",
			Reason:  "Set up a workspace and select an environment before starting Orbit.",
		})
	} else if !dstatus.Running {
		actions = append(actions, cli.JSONAction{
			Command: "orbit up --json",
			Reason:  "Start the selected environment and its daemon.",
		})
	}
	if !setup.SelectionRequired && !dstatus.ConfigStale && !dstatus.UpdateAvailable {
		actions = cli.MergeActions(actions, statusRecoveryActions(running))
	}
	setupMessage := ""
	if setup.Required {
		setupMessage = setup.Message
	}
	selectionMessage := ""
	if setup.SelectionRequired {
		selectionMessage = setup.Message
	}
	return cli.WriteJSONSuccess(w, command, statusJSONData{
		SetupRequired:     setup.Required,
		SelectionRequired: setup.SelectionRequired,
		SetupMessage:      setupMessage,
		SelectionMessage:  selectionMessage,
		Environment:       setup.Selection,
		Daemon:            dstatus,
		Resources:         resources,
	}, actions)
}

func printEnvironmentSelectionRecovery(selection environmentSelection) {
	fmt.Printf("%s environment %q is no longer available\n",
		cli.Yellow.Sprint("!"), selection.SelectedName)
	if len(selection.Environments) == 0 {
		fmt.Println("  Next: orbit env sync")
		return
	}
	if len(selection.Environments) == 1 {
		fmt.Printf("  Available environment: %s\n", selection.Environments[0].Name)
		fmt.Printf("  Next: %s\n", environmentSwitchCommand(selection.Environments[0].Name, false))
		return
	}
	fmt.Println("  Choose an available environment:")
	for _, environment := range selection.Environments {
		fmt.Printf("    %s\n", environmentSwitchCommand(environment.Name, false))
	}
}

func applyRuntimeStatus(target *jsonService, source daemon.ResourceStatus, running map[string]daemon.ResourceStatus) {
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

func statusDependencyBlocker(service daemon.ResourceStatus, running map[string]daemon.ResourceStatus) *daemon.ResourceStatus {
	if service.State != "pending" || len(service.PendingDependencies) == 0 {
		return nil
	}
	status := &daemon.StatusResponse{Resources: make([]daemon.ResourceStatus, 0, len(running))}
	for _, candidate := range running {
		status.Resources = append(status.Resources, candidate)
	}
	return terminalDependencyBlocker(status, service.PendingDependencies)
}

func statusRecoveryTargets(running map[string]daemon.ResourceStatus) []string {
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

func statusRecoveryActions(running map[string]daemon.ResourceStatus) []cli.JSONAction {
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
		fmt.Printf("  %s environment changes pending — orbit daemon restart to apply\n",
			cli.Faint.Sprint("⚠"))
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
