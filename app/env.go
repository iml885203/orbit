package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/spf13/cobra"
)

// parseOnOff accepts "on"/"off"/"true"/"false"/"1"/"0" (case-insensitive).
func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "1":
		return true, nil
	case "off", "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("expected on|off, got %q", s)
	}
}

func envCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environment configs",
	}
	useCmd := &cobra.Command{
		Use:    "use <path>",
		Short:  "Set the active environment config",
		Args:   cobra.ExactArgs(1),
		RunE:   runEnvUse,
		Hidden: true,
	}
	cmd.AddCommand(useCmd)
	currentCmd := &cobra.Command{
		Use:    "current",
		Short:  "Show the active environment config",
		Hidden: true,
		Run: func(_ *cobra.Command, _ []string) {
			path := readCurrentEnv()
			if path == "" {
				fmt.Println("No environment set. Use: orbit switch <env>")
				return
			}
			fmt.Println(path)
		},
	}
	cmd.AddCommand(currentCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available environment configs",
		RunE:  runEnvList,
	})
	cmd.AddCommand(envSyncCmd())
	toggleCmd := &cobra.Command{
		Use:    "toggle <service> <var> <on|off>",
		Short:  "Flip a pre-declared env-var toggle on a service",
		Args:   cobra.ExactArgs(3),
		RunE:   runEnvToggle,
		Hidden: true,
	}
	cmd.AddCommand(toggleCmd)
	return cmd
}

func runEnvToggle(_ *cobra.Command, args []string) error {
	service, varName, state := args[0], args[1], args[2]
	enabled, err := parseOnOff(state)
	if err != nil {
		return err
	}
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return err
	}
	if err := client.SetEnvToggle(service, varName, enabled); err != nil {
		return err
	}
	stateStr := "off"
	if enabled {
		stateStr = "on"
	}
	fmt.Printf("✓ %s/%s → %s\n", service, varName, stateStr)
	return nil
}

func runEnvUse(_ *cobra.Command, args []string) error {
	abs, err := resolveEnvArg(args[0])
	if err != nil {
		return err
	}

	if err := writeCurrentEnv(abs); err != nil {
		return fmt.Errorf("writing current env: %w", err)
	}

	_, alive := daemon.IsDaemonRunning()
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildEnvUseJSONData(abs, alive), envUseRecommendedActions(alive))
	}

	fmt.Printf("Environment set to: %s\n", abs)
	if alive {
		fmt.Println("⚠ daemon still using previous env. Apply with: orbit daemon restart")
	}
	return nil
}

type envUseJSONData struct {
	Operation       string `json:"operation"`
	SelectedEnv     string `json:"selected_env"`
	EnvName         string `json:"env_name"`
	DaemonRunning   bool   `json:"daemon_running"`
	RestartRequired bool   `json:"restart_required"`
}

func buildEnvUseJSONData(selectedEnv string, daemonRunning bool) envUseJSONData {
	return envUseJSONData{
		Operation:       "env_use",
		SelectedEnv:     selectedEnv,
		EnvName:         filepath.Base(selectedEnv),
		DaemonRunning:   daemonRunning,
		RestartRequired: daemonRunning,
	}
}

func envUseRecommendedActions(daemonRunning bool) []cli.JSONAction {
	if !daemonRunning {
		return nil
	}
	return []cli.JSONAction{{
		Command:     "orbit daemon restart --json",
		Reason:      "Apply the selected environment to the running daemon.",
		Destructive: false,
	}}
}

// resolveEnvArg accepts either an explicit path, a bare name ("example"),
// or a short filename ("example.yaml"), resolving against ~/.orbit/envs/.
func resolveEnvArg(arg string) (string, error) {
	candidates := []string{arg}
	if !strings.ContainsAny(arg, `/\`) {
		name := arg
		if !strings.HasSuffix(name, ".yaml") {
			name += ".yaml"
		}
		candidates = append(candidates, filepath.Join(envsDestDir(), name))
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}
	return "", fmt.Errorf("env not found: %s (looked under %s)", arg, envsDestDir())
}

func runEnvList(_ *cobra.Command, _ []string) error {
	dir := envsDestDir()
	names := daemonsrv.ListEnvYamls(dir)
	if names == nil {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			fmt.Printf("No envs found at %s. Run `orbit init` or `orbit env sync`.\n", dir)
			return nil
		}
	}

	current := readCurrentEnv()
	for _, name := range names {
		abs := filepath.Join(dir, name)
		marker := "  "
		if abs == current {
			marker = cli.Green.Sprint("* ")
		}
		fmt.Printf("%s%s\n", marker, name)
	}
	return nil
}

func writeCurrentEnv(absPath string) error {
	return os.WriteFile(daemonsrv.CurrentEnvPath(), []byte(absPath+"\n"), 0644)
}

func readCurrentEnv() string {
	return daemonsrv.ReadCurrentEnv()
}

func switchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "switch <env>",
		Short: "Switch env and restart daemon",
		Long:  "Set active env (by short name or path) and restart the daemon to apply it. Containers and services are left to be managed by 'orbit up' / 'orbit down'.",
		Args:  cobra.ExactArgs(1),
		RunE:  runSwitch,
	}
}

func runSwitch(_ *cobra.Command, args []string) error {
	abs, err := resolveEnvArg(args[0])
	if err != nil {
		return err
	}
	if err := writeCurrentEnv(abs); err != nil {
		return fmt.Errorf("writing current env: %w", err)
	}
	if !cli.JSONOutput {
		fmt.Printf("→ switching to %s\n", abs)
	}

	pidBefore, alive := daemon.IsDaemonRunning()
	daemonAction := "start"
	if alive {
		daemonAction = "restart"
	}

	if alive {
		if !cli.JSONOutput {
			fmt.Println("→ daemon restart")
		}
		if _, err := stopDaemon(pidBefore); err != nil {
			return err
		}
	} else if !cli.JSONOutput {
		fmt.Println("→ daemon start")
	}
	if err := ensureDaemonStarted(abs); err != nil {
		return err
	}
	if cli.JSONOutput {
		pid, running := daemon.IsDaemonRunning()
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildSwitchJSONData(switchJSONOptions{
			SelectedEnv:          abs,
			DaemonAction:         daemonAction,
			DaemonRunningBefore:  alive,
			DaemonRunningAfter:   running,
			PID:                  pid,
			ConfigPath:           abs,
			RequestedConfigApply: true,
		}), []cli.JSONAction{cli.StatusAction()})
	}
	fmt.Printf("Daemon running. Dashboard: http://localhost:%d\n", daemon.DashboardPort())
	fmt.Printf("✓ switched to %s\n", filepath.Base(abs))
	return nil
}

type switchJSONOptions struct {
	SelectedEnv          string
	DaemonAction         string
	DaemonRunningBefore  bool
	DaemonRunningAfter   bool
	PID                  int
	ConfigPath           string
	RequestedConfigApply bool
}

type switchJSONData struct {
	Operation            string `json:"operation"`
	SelectedEnv          string `json:"selected_env"`
	EnvName              string `json:"env_name"`
	DaemonAction         string `json:"daemon_action"`
	DaemonRunningBefore  bool   `json:"daemon_running_before"`
	DaemonRunningAfter   bool   `json:"daemon_running_after"`
	PID                  int    `json:"pid,omitempty"`
	ConfigPath           string `json:"config_path"`
	Dashboard            string `json:"dashboard,omitempty"`
	RequestedConfigApply bool   `json:"requested_config_apply"`
}

func buildSwitchJSONData(opts switchJSONOptions) switchJSONData {
	out := switchJSONData{
		Operation:            "switch",
		SelectedEnv:          opts.SelectedEnv,
		EnvName:              filepath.Base(opts.SelectedEnv),
		DaemonAction:         opts.DaemonAction,
		DaemonRunningBefore:  opts.DaemonRunningBefore,
		DaemonRunningAfter:   opts.DaemonRunningAfter,
		PID:                  opts.PID,
		ConfigPath:           opts.ConfigPath,
		RequestedConfigApply: opts.RequestedConfigApply,
	}
	if opts.DaemonRunningAfter {
		out.Dashboard = fmt.Sprintf("http://localhost:%d", daemon.DashboardPort())
	}
	return out
}

func resolveConfigFile() string {
	if path := readCurrentEnv(); path != "" {
		return path
	}
	if distribution.DefaultEnv == "" {
		return ""
	}
	return filepath.Join(envsDestDir(), distribution.DefaultEnv)
}
