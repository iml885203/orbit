package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/spf13/cobra"
)

var envInfoShowSecrets bool

func envInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info [environment]",
		Short: "Show declared resource endpoints and environment metadata",
		Long: "Report the named environment's identity and each declared resource's ports and URL.\n" +
			"Without an argument, report the active environment. This describes configuration;\n" +
			"use 'orbit inspect --json' for runtime readiness.\n\n" +
			"Declared values come from the environment file; observed values come from the\n" +
			"running daemon and are reported only when it serves this same environment, so\n" +
			"a caller is never handed another stack's ports as if they were its own.",
		Example: "  orbit env info\n  orbit env info local/demo\n  orbit env info --json",
		Args:    cobra.MaximumNArgs(1),
		RunE:    runEnvInfo,
	}
	cmd.Flags().BoolVar(&envInfoShowSecrets, "show-secrets", false, "include resource environment values (may contain credentials)")
	return cmd
}

type envInfoJSONData struct {
	Operation  string                     `json:"operation"`
	Env        envInfoIdentity            `json:"env"`
	Daemon     envInfoDaemon              `json:"daemon"`
	Containers map[string]envInfoResource `json:"containers"`
	Services   map[string]envInfoResource `json:"services"`
}

type envInfoIdentity struct {
	Identity    string `json:"identity"`
	Name        string `json:"name"`
	ConfigPath  string `json:"config_path"`
	Source      string `json:"source"`
	SourceName  string `json:"source_name,omitempty"`
	SourceType  string `json:"source_type,omitempty"`
	Location    string `json:"source_location,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	Ref         string `json:"ref,omitempty"`
	ResolvedRef string `json:"resolved_ref,omitempty"`
	Commit      string `json:"commit,omitempty"`
	Selected    bool   `json:"selected"`
	Running     bool   `json:"running"`
	ProjectRoot string `json:"project_root,omitempty"`
}

type envInfoDaemon struct {
	Running bool `json:"running"`
	// ConfigMatch is false when the daemon serves a different environment;
	// observed values are withheld in that case rather than misattributed.
	ConfigMatch bool `json:"config_match"`
}

type envInfoPort struct {
	Declared int `json:"declared,omitempty"`
	Target   int `json:"target,omitempty"`
	Observed int `json:"observed,omitempty"`
}

type envInfoURL struct {
	Declared string `json:"declared,omitempty"`
	Observed string `json:"observed,omitempty"`
}

type envInfoResource struct {
	State           string                 `json:"state,omitempty"`
	Ports           map[string]envInfoPort `json:"ports,omitempty"`
	URL             *envInfoURL            `json:"url,omitempty"`
	EnvironmentKeys []string               `json:"environment_keys,omitempty"`
	Environment     map[string]string      `json:"environment,omitempty"`
}

func runEnvInfo(_ *cobra.Command, args []string) error {
	target := configFile
	if len(args) == 1 {
		resolved, err := resolveEnvArg(args[0])
		if err != nil {
			return err
		}
		target = resolved
	}
	if err := applySourceWorkspace(target); err != nil {
		return err
	}
	cfg, err := config.Load(target)
	if err != nil {
		return err
	}
	daemonInfo, status := observedEnvironmentStatus(target)
	data := buildEnvInfoJSONData(cfg, target, daemonInfo, status, envInfoShowSecrets)
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), data, envInfoActions(daemonInfo))
	}
	printEnvInfoHuman(data)
	return nil
}

// observedEnvironmentStatus returns the daemon's status only when the daemon
// runs this same environment. A mismatched daemon's ports and states belong
// to another stack; handing them out would be confidently wrong.
func observedEnvironmentStatus(cfgPath string) (envInfoDaemon, *daemon.StatusResponse) {
	client := daemon.NewClient(daemon.DefaultSocketPath())
	if client.Health() != nil {
		return envInfoDaemon{}, nil
	}
	status, err := client.Status()
	if err != nil {
		return envInfoDaemon{Running: true}, nil
	}
	if daemon.CheckConfigMatch(cfgPath, status.ConfigPath) != nil {
		return envInfoDaemon{Running: true}, nil
	}
	return envInfoDaemon{Running: true, ConfigMatch: true}, status
}

func buildEnvInfoJSONData(
	cfg *config.Config,
	cfgPath string,
	daemonInfo envInfoDaemon,
	status *daemon.StatusResponse,
	showSecrets bool,
) envInfoJSONData {
	observed := map[string]*daemon.ResourceStatus{}
	if status != nil {
		for i := range status.Resources {
			observed[status.Resources[i].Name] = &status.Resources[i]
		}
	}

	abs, err := filepath.Abs(cfgPath)
	if err != nil {
		abs = cfgPath
	}
	data := envInfoJSONData{
		Operation:  "env_info",
		Env:        envInfoIdentity{Identity: abs, Name: daemonsrv.EnvShortName(abs), ConfigPath: abs, Source: environmentContextKind(abs)},
		Daemon:     daemonInfo,
		Containers: map[string]envInfoResource{},
		Services:   map[string]envInfoResource{},
	}
	if source, identity, managed := managedSourceForPath(abs); managed {
		data.Env.Identity = identity
		data.Env.Source = "managed"
		data.Env.SourceName = source.Name
		data.Env.SourceType = source.Type
		data.Env.Location = source.Location()
		data.Env.Workspace = source.Workspace
		data.Env.Ref = source.Ref
		data.Env.ResolvedRef = source.ResolvedRef
		data.Env.Commit = source.Commit
		data.Env.Selected = sameFilePath(abs, readCurrentEnv())
	}
	data.Env.Running = daemonInfo.Running && daemonInfo.ConfigMatch
	if status != nil && status.Context.Kind != "" {
		data.Env.Source = status.Context.Kind
		data.Env.ProjectRoot = status.Context.ProjectRoot
	} else if data.Env.Source == "project" {
		data.Env.Name = projectContextName(abs)
		data.Env.ProjectRoot = filepath.Dir(abs)
	}
	for name, container := range cfg.Containers {
		data.Containers[name] = buildEnvInfoResource(
			container.Ports, container.Environment, container.URL, observed[name], showSecrets,
		)
	}
	for name, service := range cfg.Services {
		data.Services[name] = buildEnvInfoResource(
			service.Ports, service.Env, service.URL, observed[name], showSecrets,
		)
	}
	return data
}

func buildEnvInfoResource(
	ports map[string]config.PortDef,
	environment map[string]string,
	declaredURL string,
	observed *daemon.ResourceStatus,
	showSecrets bool,
) envInfoResource {
	resource := envInfoResource{}
	if len(ports) > 0 {
		resource.Ports = map[string]envInfoPort{}
		for alias, def := range ports {
			port := envInfoPort{Declared: def.Host, Target: def.Target}
			if observed != nil {
				port.Observed = observed.Ports[alias]
			}
			resource.Ports[alias] = port
		}
	}
	if observed != nil {
		resource.State = observed.State
	}
	observedURL := ""
	if observed != nil {
		observedURL = observed.URL
	}
	if declaredURL != "" || observedURL != "" {
		resource.URL = &envInfoURL{Declared: declaredURL, Observed: observedURL}
	}
	if len(environment) > 0 {
		keys := make([]string, 0, len(environment))
		for key := range environment {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		resource.EnvironmentKeys = keys
		if showSecrets {
			resource.Environment = environment
		}
	}
	return resource
}

func envInfoActions(daemonInfo envInfoDaemon) []cli.JSONAction {
	if daemonInfo.Running && daemonInfo.ConfigMatch {
		return nil
	}
	return []cli.JSONAction{{
		Command:     "orbit up --json",
		Reason:      "Start this environment to observe actual states, ports, and URLs.",
		Destructive: false,
	}}
}

func printEnvInfoHuman(data envInfoJSONData) {
	fmt.Printf("Environment: %s\n", data.Env.Identity)
	fmt.Printf("Source: %s\n", data.Env.Source)
	if data.Env.SourceName != "" {
		fmt.Printf("Source name: %s [%s]\nLocation: %s\n", data.Env.SourceName, data.Env.SourceType, data.Env.Location)
		if data.Env.Workspace != "" {
			fmt.Printf("Workspace: %s\n", data.Env.Workspace)
		}
		if data.Env.Ref != "" || data.Env.ResolvedRef != "" {
			fmt.Printf("Revision: %s", data.Env.ResolvedRef)
			if data.Env.Ref != "" && data.Env.Ref != data.Env.ResolvedRef {
				fmt.Printf(" (requested %s)", data.Env.Ref)
			}
			fmt.Println()
		}
		if data.Env.Commit != "" {
			fmt.Printf("Commit: %s\n", data.Env.Commit)
		}
	}
	fmt.Printf("Selected: %t\nRunning: %t\n", data.Env.Selected, data.Env.Running)
	fmt.Printf("Config: %s\n", data.Env.ConfigPath)
	if data.Env.ProjectRoot != "" {
		fmt.Printf("Project root: %s\n", data.Env.ProjectRoot)
	}
	switch {
	case data.Daemon.Running && data.Daemon.ConfigMatch:
		fmt.Println("Daemon: running, serving this environment")
	case data.Daemon.Running:
		fmt.Println("Daemon: running a different environment — values below are declared only")
	default:
		fmt.Println("Daemon: not running — values below are declared only")
	}
	printEnvInfoSection("Containers", data.Containers)
	printEnvInfoSection("Services", data.Services)
	if !data.Daemon.Running || !data.Daemon.ConfigMatch {
		fmt.Println()
		fmt.Println("  Next: orbit up")
	}
}

func printEnvInfoSection(title string, resources map[string]envInfoResource) {
	if len(resources) == 0 {
		return
	}
	names := make([]string, 0, len(resources))
	for name := range resources {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("\n%s:\n", title)
	for _, name := range names {
		resource := resources[name]
		line := "  " + name
		if resource.State != "" {
			line += "  [" + resource.State + "]"
		}
		fmt.Println(line)
		aliases := make([]string, 0, len(resource.Ports))
		for alias := range resource.Ports {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		for _, alias := range aliases {
			port := resource.Ports[alias]
			detail := fmt.Sprintf("    %s: declared %d", alias, port.Declared)
			if port.Observed != 0 {
				detail += fmt.Sprintf(", observed %d", port.Observed)
			}
			fmt.Println(detail)
		}
		if resource.URL != nil {
			if resource.URL.Observed != "" {
				fmt.Printf("    url: %s\n", resource.URL.Observed)
			} else if resource.URL.Declared != "" {
				fmt.Printf("    url: %s (declared)\n", resource.URL.Declared)
			}
		}
		if len(resource.EnvironmentKeys) > 0 && resource.Environment == nil {
			fmt.Printf("    env: %d keys (values withheld; rerun with --show-secrets)\n", len(resource.EnvironmentKeys))
		}
		for _, key := range resource.EnvironmentKeys {
			if resource.Environment != nil {
				fmt.Printf("    env: %s=%s\n", key, resource.Environment[key])
			}
		}
	}
}
