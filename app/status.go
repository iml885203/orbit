package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show environment status",
		RunE:  runStatus,
	}
}

func runStatus(_ *cobra.Command, _ []string) error {
	selection := readEnvironmentSelection()
	cfg, cfgErr := config.Load(configFile)

	client := daemon.NewClient(daemon.DefaultSocketPath())
	daemonRunning := client.Health() == nil
	daemonRunningForConfig := daemonRunning

	dstatus := daemonStatus{Running: daemonRunning}
	running := make(map[string]daemon.ResourceStatus)
	if daemonRunning {
		if status, err := client.Status(); err == nil {
			if mismatch := daemon.CheckConfigMatch(configFile, status.ConfigPath); mismatch != nil {
				if !usesDiscoveredProjectConfig(configFile) {
					return mismatch
				}
				daemonRunningForConfig = false
				dstatus.ContextMismatch = true
				dstatus.ConfigPath = status.ConfigPath
				dstatus.RunningEnvironment = projectContextName(status.ConfigPath)
			} else {
				for i := range status.Resources {
					running[status.Resources[i].Name] = status.Resources[i]
				}
				dstatus.ConfigStale = status.ConfigStale
				dstatus.ConfigStaleReason = status.ConfigStaleReason
			}
		}
		if v, err := currentDaemonVersion(client); err == nil {
			dstatus.Version = v.Running
			dstatus.OnDisk = v.OnDisk
			dstatus.OnDiskPath = v.OnDiskPath
			dstatus.UpdateAvailable = v.UpdateAvailable
		}
	}

	setup := statusSetupState{Selection: activeEnvironmentSelection(selection, configFile)}
	if dstatus.ContextMismatch {
		setup.Selection.ContextSwitchRequired = true
		setup.Selection.RunningName = dstatus.RunningEnvironment
		setup.Selection.RunningPath = dstatus.ConfigPath
	}
	if environmentSelectionBlocksConfig(selection, configFile) {
		setup.SelectionRequired = true
		setup.Message = environmentSelectionMessage(selection)
		if cli.JSONOutput {
			return writeStatusJSON(os.Stdout, commandString(), nil, running, dstatus, setup)
		}
		printEnvironmentSelectionRecovery(selection)
		if daemonRunning {
			fmt.Println()
			printEnvironmentHeader(os.Stdout, selection.SelectedName, dstatus)
			printStatusEnvironmentSource(setup.Selection)
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
		_, _ = cli.Faint.Println("  Next: orbit init")
		_, _ = cli.Faint.Println("  Or: orbit env sync --url <git-url>   sync env repository")
		_, _ = cli.Faint.Println("  Or: orbit up                          start the configured environment")
		if cfgErr != nil {
			_, _ = cli.Faint.Println("  Current config issue:")
			_, _ = cli.Faint.Printf("    %s\n", cfgErr)
		}
		return nil
	}

	if !daemonRunningForConfig {
		running = persistedRuntimeStatus(configFile, cfg)
	}

	if cli.JSONOutput {
		return writeStatusJSON(os.Stdout, commandString(), cfg, running, dstatus, setup)
	}

	name := setup.Selection.SelectedName
	if name == "" {
		name = daemonsrv.EnvShortName(configFile)
	}
	printEnvironmentHeader(os.Stdout, name, dstatus)
	printStatusEnvironmentSource(setup.Selection)

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
	if dstatus.ContextMismatch {
		tips = []string{fmt.Sprintf(
			"orbit up                  stop %s and start this project",
			dstatus.RunningEnvironment,
		)}
	} else if dstatus.ConfigStale {
		tips = []string{"orbit env apply           apply changes and restore running resources"}
	} else if dstatus.UpdateAvailable {
		tips = []string{orbitRestartCommand(false) + "      apply the Orbit update"}
	} else {
		if primary := statusPrimaryOpenableResource(running); primary != nil {
			openableServices = []string{primary.Name}
		}
		tips = buildTips(daemonRunning, stoppedInfra, stoppedServices, statusRecoveryTips(running), openableServices)
	}
	if len(tips) > 0 {
		fmt.Println()
		for _, tip := range tips {
			_, _ = cli.Faint.Printf("  %s\n", tip)
		}
	}

	return nil
}

func printStatusEnvironmentSource(selection environmentSelection) {
	if source := managedEnvironmentSourceLabel(selection); source != "" {
		fmt.Printf("  Source: %s\n\n", source)
	}
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
	case "reconciling":
		return "last seen running; orbit up will reconcile it"
	case "pending":
		if blocker := statusDependencyBlocker(svc, running); blocker != nil {
			detail := "blocked by " + blocker.Name
			if reason := serviceFailureReason(*blocker); reason != "" {
				detail += " — " + reason
			}
			return detail
		}
	}
	if svc.LastRestart != nil && svc.LastRestart.Source == "external" {
		return "restarted outside Orbit at " + svc.LastRestart.StartedAt.Local().Format(time.Kitchen)
	}
	return ""
}

func persistedRuntimeStatus(configPath string, cfg *config.Config) map[string]daemon.ResourceStatus {
	resources := make(map[string]daemon.ResourceStatus)
	if cfg == nil {
		return resources
	}
	state, err := daemonsrv.NewStateFile(daemonsrv.DefaultStatePath()).Read()
	if err != nil || !sameFilePath(state.ConfigPath, configPath) {
		return resources
	}
	for name, entry := range state.Services {
		if entry.State == "stopped" {
			continue
		}
		kind := daemon.ResourceKind(entry.Kind)
		if kind == daemon.ResourceKindService {
			record, exists := state.Processes[name]
			if !exists || !daemon.IsProcessAlive(record.PID) {
				continue
			}
		}
		resource := daemon.ResourceStatus{
			Name:  name,
			Kind:  kind,
			State: "reconciling",
		}
		switch kind {
		case daemon.ResourceKindContainer:
			if definition := cfg.Containers[name]; definition != nil {
				resource.Role = definition.ResolveKind()
				resource.Ports = configPortNumbers(definition.Ports)
			}
		case daemon.ResourceKindService:
			if definition := cfg.Services[name]; definition != nil {
				resource.Role = definition.ResolveKind()
				resource.Ports = configPortNumbers(definition.Ports)
				resource.URL = definition.URL
			}
		default:
			continue
		}
		resources[name] = resource
	}
	return resources
}

func configPortNumbers(ports map[string]config.PortDef) map[string]int {
	numbers := make(map[string]int, len(ports))
	for name, definition := range ports {
		numbers[name] = definition.Host
	}
	return numbers
}

func serviceFailureReason(svc daemon.ResourceStatus) string {
	reason := svc.StateReason
	if svc.FailureEvidence != "" && svc.FailureEvidence != reason {
		if reason != "" {
			return reason + " — " + svc.FailureEvidence
		}
		return svc.FailureEvidence
	}
	if svc.StateReason != "" {
		return reason
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

func buildTips(daemonRunning, stoppedInfra bool, stoppedServices, recoveryTips, openableServices []string) []string {
	var tips []string

	// Scenario 1: nothing running
	if !daemonRunning {
		tips = append(tips, "orbit up                  start environment")
		return tips
	}

	// Scenario 5: degraded — most urgent
	if len(recoveryTips) > 0 {
		return recoveryTips
	}

	// A stopped environment has one normal recovery path. Partial-start flags
	// remain discoverable in help, but status must not lead a user into a
	// container-only intermediate state.
	if stoppedInfra || len(stoppedServices) > 0 {
		tips = append(tips, "orbit up                  start environment")
		return tips
	}

	// Healthy services with URLs.
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
	Name                 string                       `json:"name"`
	Kind                 string                       `json:"kind"`
	Role                 string                       `json:"role,omitempty"`
	State                string                       `json:"state"`
	StateReason          string                       `json:"state_reason,omitempty"`
	FailureEvidence      string                       `json:"failure_evidence,omitempty"`
	PortConflict         *daemon.ResourcePortConflict `json:"port_conflict,omitempty"`
	LogsAvailable        bool                         `json:"logs_available,omitempty"`
	PendingDependencies  []string                     `json:"pending_dependencies,omitempty"`
	BlockedBy            string                       `json:"blocked_by,omitempty"`
	URL                  string                       `json:"url,omitempty"`
	Ports                map[string]int               `json:"ports,omitempty"`
	StartupTime          string                       `json:"startup_time,omitempty"`
	Uptime               string                       `json:"uptime,omitempty"`
	RestartCount         int                          `json:"restart_count"`
	ExternalRestartCount int                          `json:"external_restart_count"`
	LastRestart          *daemon.ResourceRestart      `json:"last_restart,omitempty"`
}

type daemonStatus struct {
	Running            bool   `json:"running"`
	Version            string `json:"version,omitempty"`
	OnDisk             string `json:"on_disk,omitempty"`
	OnDiskPath         string `json:"on_disk_path,omitempty"`
	UpdateAvailable    bool   `json:"update_available"`
	ConfigPath         string `json:"config_path,omitempty"`
	RunningEnvironment string `json:"running_environment,omitempty"`
	ContextMismatch    bool   `json:"context_mismatch,omitempty"`
	ConfigStale        bool   `json:"config_stale,omitempty"`
	ConfigStaleReason  string `json:"config_stale_reason,omitempty"`
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
				Role:  resource.Role,
				State: resource.State,
				URL:   resource.URL,
				Ports: resource.Ports,
			}
			applyRuntimeStatus(&svc, resource, running)
			resources = append(resources, svc)
		}
	} else if cfg != nil {
		for name, c := range cfg.Containers {
			svc := jsonService{Name: name, Kind: "container", Role: c.ResolveKind(), State: "stopped"}
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
			svc := jsonService{Name: name, Kind: "service", Role: s.ResolveKind(), State: "stopped", URL: s.URL}
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
	} else if dstatus.ContextMismatch {
		actions = append(actions, cli.JSONAction{
			Command: "orbit up --json",
			Reason: fmt.Sprintf(
				"Stop %s and start the current project.",
				dstatus.RunningEnvironment,
			),
		})
	} else if dstatus.ConfigStale {
		actions = append(actions, cli.JSONAction{
			Command: "orbit env apply --json",
			Reason:  "Apply the selected environment changes and restore running resources.",
		})
	} else if dstatus.UpdateAvailable {
		actions = append(actions, cli.JSONAction{
			Command: orbitRestartCommand(true),
			Reason:  "Apply the Orbit update before operating resources.",
		})
	} else if setup.Required {
		actions = append(actions, cli.JSONAction{
			Command: "orbit init --yes --json",
			Reason:  "Set up a workspace and select an environment before starting Orbit.",
		})
	} else if !dstatus.Running {
		actions = append(actions, cli.JSONAction{
			Command: "orbit up --json",
			Reason:  "Start the selected environment.",
		})
	}
	if !setup.SelectionRequired && !dstatus.ContextMismatch && !dstatus.ConfigStale && !dstatus.UpdateAvailable {
		recoveryActions := statusRecoveryActions(running)
		if len(recoveryActions) > 0 {
			actions = cli.MergeActions(actions, recoveryActions)
		} else if statusHasStoppedResources(resources) {
			actions = cli.MergeActions(actions, []cli.JSONAction{{
				Command: "orbit up --json",
				Reason:  "Start the selected environment.",
			}})
		} else if !setup.Required {
			if action, ok := statusPrimaryOpenAction(resources); ok {
				actions = cli.MergeActions(actions, []cli.JSONAction{action})
			}
		}
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

func statusPrimaryOpenAction(resources []jsonService) (cli.JSONAction, bool) {
	running := make(map[string]daemon.ResourceStatus, len(resources))
	for _, resource := range resources {
		if resource.State != "healthy" {
			return cli.JSONAction{}, false
		}
		running[resource.Name] = daemon.ResourceStatus{
			Name:  resource.Name,
			Kind:  daemon.ResourceKind(resource.Kind),
			Role:  resource.Role,
			State: resource.State,
			URL:   resource.URL,
		}
	}
	resource := statusPrimaryOpenableResource(running)
	if resource == nil {
		return cli.JSONAction{}, false
	}
	return cli.JSONAction{
		Command: "orbit open " + resource.Name + " --json",
		Reason:  fmt.Sprintf("Open %s at %s.", resource.Name, resource.URL),
	}, true
}

func statusPrimaryOpenableResource(resources map[string]daemon.ResourceStatus) *daemon.ResourceStatus {
	status := &daemon.StatusResponse{
		Resources: make([]daemon.ResourceStatus, 0, len(resources)),
	}
	for _, resource := range resources {
		status.Resources = append(status.Resources, resource)
	}
	return primaryOpenableResource(nil, status)
}

func statusHasStoppedResources(resources []jsonService) bool {
	for _, resource := range resources {
		if resource.State == "stopped" || resource.State == "pending" {
			return true
		}
	}
	return false
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
	if source.Role != "" {
		target.Role = source.Role
	}
	if source.URL != "" {
		target.URL = source.URL
	}
	if source.Ports != nil {
		target.Ports = source.Ports
	}
	if source.State == "degraded" {
		target.StateReason = source.StateReason
		target.FailureEvidence = source.FailureEvidence
		target.PortConflict = source.PortConflict
	}
	target.PendingDependencies = append([]string{}, source.PendingDependencies...)
	target.BlockedBy = source.BlockedBy
	target.LogsAvailable = source.LogsAvailable
	target.StartupTime = source.StartupTime
	target.Uptime = source.Uptime
	target.RestartCount = source.RestartCount
	target.ExternalRestartCount = source.ExternalRestartCount
	target.LastRestart = source.LastRestart
	if blocker := statusDependencyBlocker(source, running); blocker != nil {
		target.BlockedBy = blocker.Name
	}
}

func statusDependencyBlocker(service daemon.ResourceStatus, running map[string]daemon.ResourceStatus) *daemon.ResourceStatus {
	if service.BlockedBy != "" {
		blocker, ok := running[service.BlockedBy]
		if ok {
			return &blocker
		}
	}
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
		if blocker := terminalRuntimeBlocker(service, running); blocker != nil {
			targets[blocker.Name] = true
			continue
		}
		if service.State == "degraded" && (service.HealthProgress == nil || !service.HealthProgress.Recovering) {
			targets[service.Name] = true
		}
	}
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func terminalRuntimeBlocker(service daemon.ResourceStatus, running map[string]daemon.ResourceStatus) *daemon.ResourceStatus {
	seen := make(map[string]bool, len(running))
	current := service
	for {
		if seen[current.Name] {
			return nil
		}
		seen[current.Name] = true
		blocker := statusDependencyBlocker(current, running)
		if blocker == nil {
			return nil
		}
		if blocker.BlockedBy == "" {
			return blocker
		}
		current = *blocker
	}
}

func statusRecoveryActions(running map[string]daemon.ResourceStatus) []cli.JSONAction {
	var actions []cli.JSONAction
	dockerUnavailableAdded := false
	for _, name := range statusRecoveryTargets(running) {
		service := running[name]
		if service.StateReason == engine.DockerObservationUnavailableReason {
			if !dockerUnavailableAdded {
				actions = append(actions, cli.JSONAction{
					Command: "orbit doctor --json",
					Reason:  "Check why Docker is unavailable; Orbit reconnects automatically when it returns.",
				})
				dockerUnavailableAdded = true
			}
			continue
		}
		if service.PortConflict != nil {
			actions = append(actions, resourcePortConflictActions(service.PortConflict)...)
			continue
		}
		if service.State == "stopped" {
			actions = append(actions, cli.JSONAction{
				Command: "orbit up " + name + " --json",
				Reason:  "Start " + name + ", which is blocking dependent services.",
			})
			continue
		}
		if service.LogsAvailable {
			actions = append(actions, cli.JSONAction{
				Command: "orbit logs " + name + " --json",
				Reason:  "Review the exit output for " + name + " before retrying it.",
			})
			continue
		}
		actions = append(actions, cli.JSONAction{
			Command: "orbit restart " + name + " --json",
			Reason:  "Retry " + name + "; no process output is available to review.",
		})
	}
	return actions
}

func statusRecoveryTips(running map[string]daemon.ResourceStatus) []string {
	var tips []string
	dockerUnavailableAdded := false
	for _, name := range statusRecoveryTargets(running) {
		service := running[name]
		if service.StateReason == engine.DockerObservationUnavailableReason {
			if !dockerUnavailableAdded {
				tips = append(tips, "orbit doctor              diagnose Docker; Orbit reconnects automatically")
				dockerUnavailableAdded = true
			}
			continue
		}
		if service.PortConflict != nil {
			if service.PortConflict.InspectCommand != "" {
				tips = append(tips, fmt.Sprintf("%s  inspect port %d owner", service.PortConflict.InspectCommand, service.PortConflict.Port))
			}
			continue
		}
		if service.State == "stopped" {
			tips = append(tips, fmt.Sprintf("orbit up %-16s  restore blocked services", name))
			continue
		}
		if service.LogsAvailable {
			tips = append(tips, fmt.Sprintf("orbit logs %-14s  review exit output", name))
			continue
		}
		tips = append(tips, fmt.Sprintf("orbit restart %-11s  retry service", name))
	}
	return tips
}

func printEnvironmentHeader(w io.Writer, name string, s daemonStatus) {
	_, _ = cli.Bold.Fprintln(w, "ENVIRONMENT")
	if name == "" {
		name = "selected configuration"
	}
	_, _ = fmt.Fprintf(w, "  %s\n", name)
	if s.UpdateAvailable && s.OnDisk != "" {
		_, _ = fmt.Fprintf(w, "  %s Orbit update ready — %s to apply\n",
			cli.Faint.Sprint("⚠"), orbitRestartCommand(false))
	}
	if s.ConfigStale {
		_, _ = fmt.Fprintf(w, "  %s environment changes pending — orbit env apply\n",
			cli.Faint.Sprint("⚠"))
	}
	if s.ContextMismatch {
		_, _ = fmt.Fprintf(
			w,
			"  %s %s is running — orbit up switches to this project\n",
			cli.Faint.Sprint("⚠"),
			s.RunningEnvironment,
		)
	}
	_, _ = fmt.Fprintln(w)
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
