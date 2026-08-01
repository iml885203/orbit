package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
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
	cmd.AddCommand(envApplyCmd())
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
		fmt.Println("Environment change ready. Apply with: orbit env apply")
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
		EnvName:         daemonsrv.EnvShortName(selectedEnv),
		DaemonRunning:   daemonRunning,
		RestartRequired: daemonRunning,
	}
}

func envUseRecommendedActions(daemonRunning bool) []cli.JSONAction {
	if !daemonRunning {
		return nil
	}
	return []cli.JSONAction{{
		Command:     "orbit env apply --json",
		Reason:      "Apply the selected environment and restore running resources.",
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
	selection := readEnvironmentSelection()
	actions := environmentSelectionActions(selection)
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildEnvListJSONData(selection), actions)
	}

	if selection.State == environmentSelectionUnavailable {
		fmt.Printf("%s environment %q is no longer available\n",
			cli.Yellow.Sprint("!"), selection.SelectedName)
	}
	if len(selection.Environments) == 0 {
		fmt.Printf("No environments found at %s.\n", envsDestDir())
		fmt.Println("  Next: orbit env sync")
		return nil
	}

	for _, environment := range selection.Environments {
		marker := "  "
		if environment.Selected {
			marker = cli.Green.Sprint("* ")
		}
		fmt.Printf("%s%s\n", marker, environment.Name)
	}
	if source := managedEnvironmentSourceLabel(selection); source != "" {
		fmt.Printf("\n  Source: %s\n", source)
	}
	if selection.State != environmentSelectionSelected {
		fmt.Println()
		if len(selection.Environments) == 1 {
			fmt.Printf("  Next: %s\n", environmentSwitchCommand(selection.Environments[0].Name, false))
		} else {
			fmt.Println("  Choose an environment:")
			for _, environment := range selection.Environments {
				fmt.Printf("    %s\n", environmentSwitchCommand(environment.Name, false))
			}
		}
	}
	return nil
}

func managedEnvironmentSourceLabel(selection environmentSelection) string {
	if selection.ManagedSource == nil {
		return ""
	}
	source := selection.ManagedSource
	revision := source.Ref
	if revision == "" {
		revision = source.Commit
	}
	commit := source.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	label := fmt.Sprintf("%s @ %s", source.URL, revision)
	if source.Ref != "" {
		label += fmt.Sprintf(" (%s)", commit)
	}
	return label
}

type envListJSONData struct {
	Operation   string               `json:"operation"`
	Environment environmentSelection `json:"environment"`
}

func buildEnvListJSONData(selection environmentSelection) envListJSONData {
	return envListJSONData{
		Operation:   "env_list",
		Environment: selection,
	}
}

func writeCurrentEnv(absPath string) error {
	return os.WriteFile(daemonsrv.CurrentEnvPath(), []byte(absPath+"\n"), 0644)
}

func readCurrentEnv() string {
	return daemonsrv.ReadCurrentEnv()
}

var switchYes bool

func switchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch <env>",
		Short: "Switch the active environment",
		Long:  "Stop resources in the current environment, select another environment by short name or path, and prepare it for `orbit up`.\n\nWhen resources are running, switch asks for confirmation before stopping them; pass --yes to confirm non-interactively.",
		Args:  cobra.ExactArgs(1),
		RunE:  runSwitch,
	}
	cmd.Flags().BoolVar(&switchYes, "yes", false, "confirm stopping the running resources without prompting")
	return cmd
}

func runSwitch(_ *cobra.Command, args []string) error {
	abs, err := resolveEnvArg(args[0])
	if err != nil {
		selection := readEnvironmentSelection()
		message := err.Error()
		if len(selection.Environments) > 0 {
			names := make([]string, 0, len(selection.Environments))
			for _, environment := range selection.Environments {
				names = append(names, environment.Name)
			}
			message += "\nAvailable: " + strings.Join(names, ", ")
		}
		return cli.WithJSONReplacementActions(errors.New(message), []cli.JSONAction{{
			Command:     "orbit env list --json",
			Reason:      "List the available environment names before retrying the switch.",
			Destructive: false,
		}})
	}
	prerequisites, prerequisitesReady, err := switchPrerequisites(abs)
	if err != nil {
		validationErr := cli.NewInvalidEnvironmentError(
			fmt.Sprintf("validate target environment %s: %v", filepath.Base(abs), err),
		)
		return cli.WithJSONActions(validationErr, []cli.JSONAction{{
			Command:     commandString(),
			Reason:      "Retry the switch after fixing the reported environment file.",
			Destructive: false,
		}})
	}

	pidBefore, alive := daemon.IsDaemonRunning()
	if alive {
		client := daemon.NewClient(daemon.DefaultSocketPath())
		proceed, err := confirmSwitchStopsRunningResources(client, args[0])
		if err != nil {
			return err
		}
		if !proceed {
			fmt.Println("Aborted.")
			return nil
		}
		if !cli.JSONOutput {
			fmt.Println("→ stopping current environment")
		}
		if _, err := client.DownAndWait(); err != nil {
			return fmt.Errorf("stop current environment: %w", err)
		}
		if _, err := stopDaemon(pidBefore); err != nil {
			return err
		}
	}

	if err := writeCurrentEnv(abs); err != nil {
		return fmt.Errorf("writing current env: %w", err)
	}
	if !cli.JSONOutput {
		fmt.Printf("→ switching to %s\n", daemonsrv.EnvShortName(abs))
	}
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildSwitchJSONData(switchJSONOptions{
			SelectedEnv:                abs,
			PreviousEnvironmentStopped: alive,
			ConfigPath:                 abs,
			Prerequisites:              prerequisites,
			PrerequisitesReady:         prerequisitesReady,
		}), switchRecommendedActions(prerequisites, prerequisitesReady))
	}
	printSwitchPrerequisites(prerequisites, prerequisitesReady)
	fmt.Printf("✓ switched to %s\n", daemonsrv.EnvShortName(abs))
	if prerequisitesReady {
		fmt.Println("  Next: orbit up")
	}
	return nil
}

// confirmSwitchStopsRunningResources keeps switch an explicit act: it is a
// machine-wide provisioning step, so a script or harness must opt in with
// --yes instead of silently stopping a stack someone else is using. A status
// probe failure skips the check — DownAndWait surfaces the real error.
func confirmSwitchStopsRunningResources(client *daemon.Client, target string) (bool, error) {
	if switchYes {
		return true, nil
	}
	status, err := client.Status()
	if err != nil {
		return true, nil
	}
	running := runningResourceNames(status)
	if len(running) == 0 {
		return true, nil
	}
	summary := fmt.Sprintf(
		"Switching to %s stops %d running resource(s): %s.",
		target, len(running), strings.Join(running, ", "),
	)
	if cli.JSONOutput {
		return false, cli.WithJSONReplacementActions(
			cli.NewConfirmationRequiredError(summary+" Rerun with --yes to confirm."),
			[]cli.JSONAction{{
				Command:     fmt.Sprintf("orbit switch %s --yes --json", target),
				Reason:      "Confirm stopping the running resources before switching.",
				Destructive: true,
			}},
		)
	}
	return cli.Confirm(summary + " Continue?"), nil
}

func runningResourceNames(status *daemon.StatusResponse) []string {
	var names []string
	for i := range status.Resources {
		switch status.Resources[i].State {
		case "stopped", "pending":
		default:
			names = append(names, status.Resources[i].Name)
		}
	}
	return names
}

type switchJSONOptions struct {
	SelectedEnv                string
	PreviousEnvironmentStopped bool
	ConfigPath                 string
	Prerequisites              []daemon.DoctorCheck
	PrerequisitesReady         bool
}

type switchJSONData struct {
	Operation                  string               `json:"operation"`
	SelectedEnv                string               `json:"selected_env"`
	EnvName                    string               `json:"env_name"`
	PreviousEnvironmentStopped bool                 `json:"previous_environment_stopped"`
	ConfigPath                 string               `json:"config_path"`
	Prerequisites              []daemon.DoctorCheck `json:"prerequisites"`
	PrerequisitesReady         bool                 `json:"prerequisites_ready"`
}

func buildSwitchJSONData(opts switchJSONOptions) switchJSONData {
	return switchJSONData{
		Operation:                  "switch",
		SelectedEnv:                opts.SelectedEnv,
		EnvName:                    daemonsrv.EnvShortName(opts.SelectedEnv),
		PreviousEnvironmentStopped: opts.PreviousEnvironmentStopped,
		ConfigPath:                 opts.ConfigPath,
		Prerequisites:              opts.Prerequisites,
		PrerequisitesReady:         opts.PrerequisitesReady,
	}
}

func switchPrerequisites(path string) ([]daemon.DoctorCheck, bool, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, false, err
	}
	checks := make([]daemon.DoctorCheck, 0)
	if len(cfg.Containers) > 0 {
		checks = append(checks, daemonsrv.DockerCheck())
	}
	checks = append(checks, daemonsrv.HostEnvironmentChecks(cfg)...)
	ready := true
	for _, check := range checks {
		if check.Status == daemon.CheckFail {
			ready = false
			break
		}
	}
	return checks, ready, nil
}

func printSwitchPrerequisites(checks []daemon.DoctorCheck, ready bool) {
	hasIssue := false
	for _, check := range checks {
		if check.Status == daemon.CheckFail || check.Status == daemon.CheckWarn {
			hasIssue = true
			break
		}
	}
	if !hasIssue {
		return
	}
	message := "review setup before `orbit up`"
	if !ready {
		message = "setup required before `orbit up`"
	}
	fmt.Printf("%s %s\n", cli.Yellow.Sprint("!"), message)
	for _, check := range checks {
		if check.Status != daemon.CheckFail && check.Status != daemon.CheckWarn {
			continue
		}
		icon := cli.Yellow.Sprint("!")
		if check.Status == daemon.CheckFail {
			icon = cli.Red.Sprint("✗")
		}
		fmt.Printf("  %s %s: %s\n", icon, check.Name, check.Message)
		if check.Hint != "" {
			_, _ = cli.Faint.Printf("      → %s\n", check.Hint)
		}
	}
}

func switchRecommendedActions(checks []daemon.DoctorCheck, ready bool) []cli.JSONAction {
	if ready {
		return []cli.JSONAction{{
			Command:     "orbit up --json",
			Reason:      "Start the selected environment.",
			Destructive: false,
		}}
	}
	return doctorRecommendedActions(&daemon.DoctorResponse{Checks: checks})
}
