package app

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsync"
	"github.com/spf13/cobra"
)

var (
	initYes     bool
	initEnvRepo string
	initEnvName string
)

type initResult struct {
	WorkspaceRoot string               `json:"workspace_root,omitempty"`
	EnvsDir       string               `json:"envs_dir"`
	EnvRepoURL    string               `json:"env_repo_url,omitempty"`
	EnvSource     string               `json:"env_source"`
	SyncedFiles   []string             `json:"synced_files"`
	ActiveEnv     string               `json:"active_env,omitempty"`
	ConfigPath    string               `json:"config_path,omitempty"`
	Checks        []daemon.DoctorCheck `json:"checks"`
	Warnings      []string             `json:"warnings"`
	Ready         bool                 `json:"ready"`
}

type initPrinter struct {
	enabled bool
}

type initCompletion struct {
	Heading      string
	Detail       string
	HumanCommand string
	JSONCommand  string
	Reason       string
	Ready        bool
}

func (p initPrinter) printf(format string, args ...any) {
	if p.enabled {
		fmt.Printf(format, args...)
	}
}

func (p initPrinter) println(args ...any) {
	if p.enabled {
		fmt.Println(args...)
	}
}

func (p initPrinter) boldln(text string) {
	if p.enabled {
		_, _ = cli.Bold.Println(text)
	}
}

func (p initPrinter) faintln(text string) {
	if p.enabled {
		_, _ = cli.Faint.Println(text)
	}
}

func (p initPrinter) warnln(text string) {
	if p.enabled {
		_, _ = cli.Yellow.Println(text)
	}
}

func (p initPrinter) warnf(format string, args ...any) {
	if p.enabled {
		_, _ = cli.Yellow.Printf(format, args...)
	}
}

func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up Orbit for first use",
		Long: `Sets up an environment source, selects an environment, and verifies your setup.
The official demo uses its defaults without questions. A custom environment asks
for a project workspace only when its selected config actually requires one.

For non-interactive setup (CI, scripts):
  orbit init --yes                                         # accept all defaults
  orbit init --yes --env-repo <url>                        # use a specific env repo
  orbit init --yes --env-repo <url> --env example          # and pick an env`,
		RunE: runInit,
	}
	cmd.Flags().BoolVarP(&initYes, "yes", "y", false, "accept defaults without prompting")
	cmd.Flags().StringVar(&initEnvRepo, "env-repo", "", "git URL of the env repo (persists to settings, skips prompt)")
	cmd.Flags().StringVar(&initEnvName, "env", "", "active env short name (e.g. example); skips the selection prompt")
	return cmd
}

func runInit(_ *cobra.Command, _ []string) error {
	if cli.JSONOutput && !initYes {
		return fmt.Errorf("--json requires --yes so init never waits for interactive input")
	}
	output := initPrinter{enabled: !cli.JSONOutput}
	if output.enabled {
		printLogo()
	}
	result := initResult{
		EnvSource:   "none",
		SyncedFiles: []string{},
		Checks:      []daemon.DoctorCheck{},
		Warnings:    []string{},
	}

	settings := daemon.LoadSettings(daemon.DefaultSettingsPath())
	settings.ApplyToEnv()

	output.boldln("Step 1: Environment source")

	currentURL := settings.Get(settingKeyEnvRepoURL)
	repoURL := currentURL
	if repoURL == "" {
		repoURL = distribution.EnvRepoURL
	}
	cwd, _ := os.Getwd()
	localEnvsDir := filepath.Join(cwd, "envs")
	localInfo, localErr := os.Stat(localEnvsDir)
	useLocalEnvs := localErr == nil && localInfo.IsDir() && initEnvRepo == ""
	// Persist only what the user explicitly typed (or passed via flag).
	// Accepting the suggested default with Enter must not pin it into
	// settings.json — an unpinned default keeps following new releases.
	explicit := false
	switch {
	case initEnvRepo != "":
		repoURL = initEnvRepo
		explicit = true
	case !useLocalEnvs && repoURL == "" && !initYes:
		if input := prompt(fmt.Sprintf("  Git URL [%s]: ", repoURL)); input != "" {
			repoURL = input
			explicit = true
		}
	}
	if explicit && repoURL != currentURL {
		if err := settings.Set(settingKeyEnvRepoURL, repoURL); err != nil {
			return fmt.Errorf("saving env repo URL: %w", err)
		}
	}

	envsDir := envsDestDir()
	var syncFailure error
	var syncWarning string
	result.EnvsDir = envsDir
	result.EnvRepoURL = repoURL
	localWorkspaceRoot := ""
	if useLocalEnvs {
		localWorkspaceRoot = cwd
		result.EnvSource = "local"
		output.printf("  Syncing local %s → %s\n", localEnvsDir, envsDir)
		syncRes, err := envsync.Sync(localEnvsDir, envsDir, envsync.Options{})
		if err != nil {
			syncFailure = err
			syncWarning = "env sync failed: " + err.Error()
			output.warnln("  ! Could not sync the local environment files")
			output.faintln("    Orbit will use an existing synced environment if one is available.")
		} else {
			result.SyncedFiles = syncRes.Written
			output.printf("  %s synced %d file(s)\n", cli.Green.Sprint("✓"), len(syncRes.Written))
		}
	} else if repoURL == "" {
		warning := "no env repo configured; sync skipped"
		result.Warnings = append(result.Warnings, warning)
		output.warnln("  ! no env repo configured — skipping sync")
		output.faintln("    set one later with `orbit env sync --url <git-url>`")
	} else {
		result.EnvSource = "remote"
		output.printf("  Syncing %s → %s\n", repoURL, envsDir)
		syncRes, err := envsync.SyncFromRepo(repoURL, envsDir, envsync.Options{})
		if err != nil {
			err = envRepoSyncError(err)
			syncFailure = err
			syncWarning = "env sync failed: " + err.Error()
			output.warnln("  ! Could not sync the environment repository")
			output.faintln("    Orbit will use an existing synced environment if one is available.")
		} else {
			result.SyncedFiles = syncRes.Written
			output.printf("  %s synced %d file(s)\n", cli.Green.Sprint("✓"), len(syncRes.Written))
		}
	}

	envFiles := listEnvFiles(envsDir)
	if len(envFiles) == 0 && syncFailure != nil {
		return finishInit(output, result, syncFailure)
	}
	if syncWarning != "" {
		result.Warnings = append(result.Warnings, syncWarning)
	}
	output.println()
	output.boldln("Step 2: Environment")
	workspaceStepShown := false
	if len(envFiles) == 0 {
		result.Warnings = append(result.Warnings, "no environment configs found in "+envsDir)
		output.warnf("  ! No environment configs found in %s\n", envsDir)
		output.faintln("    retry `orbit env sync` once the repo is reachable")
	} else {
		var chosen string
		switch {
		case initEnvName != "":
			chosen = resolveInitEnvName(initEnvName, envFiles)
			if chosen == "" {
				return fmt.Errorf("--env %q not found in %s (available: %s)",
					initEnvName, envsDir, strings.Join(envFiles, ", "))
			}
			output.printf("  %s Environment: %s\n", cli.Green.Sprint("✓"), daemonsrv.EnvShortName(chosen))
		case initYes || len(envFiles) == 1:
			chosen = pickDefault(envFiles)
			output.printf("  %s Environment: %s\n", cli.Green.Sprint("✓"), daemonsrv.EnvShortName(chosen))
		default:
			for i, f := range envFiles {
				output.printf("  %d) %s\n", i+1, daemonsrv.EnvShortName(f))
			}
			input := prompt("  Select environment [1]: ")
			idx := 0
			if input != "" {
				if n, err := strconv.Atoi(input); err == nil && n >= 1 && n <= len(envFiles) {
					idx = n - 1
				}
			}
			chosen = envFiles[idx]
			output.printf("  %s Environment: %s\n", cli.Green.Sprint("✓"), daemonsrv.EnvShortName(chosen))
		}

		absPath := filepath.Join(envsDir, chosen)
		if err := writeCurrentEnv(absPath); err != nil {
			return fmt.Errorf("writing env: %w", err)
		}
		configFile = absPath
		result.ActiveEnv = daemonsrv.EnvShortName(chosen)
		result.ConfigPath = absPath

		var err error
		workspaceStepShown, result.WorkspaceRoot, err = configureRequiredWorkspace(output, settings, absPath, localWorkspaceRoot)
		if err != nil {
			return err
		}
	}

	if err := runExtensionInitSteps(settings); err != nil {
		return err
	}

	output.println()
	if workspaceStepShown {
		output.boldln("Step 4: Health check")
	} else {
		output.boldln("Step 3: Health check")
	}
	if result.ActiveEnv != "" {
		if output.enabled {
			_ = runDoctorWithOptions(doctorOptions{})
		}
		health := doctorResponse(daemon.NewClient(daemon.DefaultSocketPath()))
		result.Checks = health.Checks
		result.Ready = doctorFailure(health, false) == nil
	}
	return finishInit(output, result, syncFailure)
}

func configureRequiredWorkspace(
	output initPrinter,
	settings *daemon.Settings,
	envPath string,
	localWorkspaceRoot string,
) (bool, string, error) {
	cfg, err := config.Load(envPath)
	if err != nil {
		return false, "", nil
	}
	var requiredBy []string
	for _, check := range daemonsrv.ServiceWorkingDirectoryChecks(cfg, nil) {
		if check.Hint == `run: orbit settings set workspace-root "$PWD"` {
			name := strings.TrimSuffix(strings.TrimPrefix(check.Name, "Working directory ("), ")")
			requiredBy = append(requiredBy, name)
		}
	}
	if len(requiredBy) == 0 {
		return false, "", nil
	}

	candidate := localWorkspaceRoot
	if candidate == "" {
		candidate = detectWorkspaceRoot(settings)
	}
	if candidate == "" && !initYes {
		cwd, getwdErr := os.Getwd()
		if getwdErr == nil {
			candidate = gitCheckoutRoot(cwd)
		}
	}
	if initYes && candidate == "" {
		return false, "", nil
	}

	output.println()
	output.boldln("Step 3: Project workspace")
	output.printf("  Environment %s needs local project files for %s.\n",
		daemonsrv.EnvShortName(envPath), strings.Join(requiredBy, ", "))
	root := candidate
	if !initYes && localWorkspaceRoot == "" {
		label := "  Project checkout or workspace root (absolute path)"
		if candidate != "" {
			label += " [" + candidate + "]"
		}
		input := prompt(label + ": ")
		if input != "" {
			root = expandHome(input)
		}
	}
	if root == "" {
		return true, "", nil
	}
	root, err = normalizeWorkspaceRoot(root)
	if err != nil {
		return false, "", err
	}
	if err := settings.Set("workspace_root", root); err != nil {
		return false, "", fmt.Errorf("saving workspace root: %w", err)
	}
	settings.ApplyToEnv()
	output.printf("  %s Project workspace: %s\n", cli.Green.Sprint("✓"), root)
	return true, root, nil
}

func gitCheckoutRoot(start string) string {
	current := filepath.Clean(start)
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func runExtensionInitSteps(settings *daemon.Settings) error {
	for _, ext := range extensions {
		if ext.CLIInit != nil && ext.CLIInit.Steps != nil {
			if err := ext.CLIInit.Steps(settings, initYes, prompt, cli.JSONOutput); err != nil {
				return err
			}
		}
	}
	return nil
}

func finishInit(output initPrinter, result initResult, syncFailure error) error {
	output.println()
	completion := buildInitCompletion(result, syncFailure)
	if completion.Ready {
		output.boldln(completion.Heading)
	} else {
		output.warnln(completion.Heading)
	}
	if completion.Detail != "" {
		output.faintln("  " + completion.Detail)
	}
	if completion.HumanCommand != "" {
		output.faintln(fmt.Sprintf("  Next: %-16s %s", completion.HumanCommand, completion.Reason))
	}

	if cli.JSONOutput {
		if !result.Ready {
			failure := initFailure(result, syncFailure)
			actions := initFailureRecommendedActions(result, failure)
			failure = cli.WithJSONReplacementActions(failure, actions)
			if err := cli.WriteJSONFailure(os.Stdout, commandString(), result, failure, actions); err != nil {
				return err
			}
			return errCLIJSONAlreadyRendered{err: failure}
		}
		return cli.WriteJSONSuccess(os.Stdout, commandString(), result, initRecommendedActions(result))
	}
	if !result.Ready {
		return initFailure(result, syncFailure)
	}
	return nil
}

func initFailure(result initResult, syncFailure error) error {
	if syncFailure != nil {
		if errors.Is(syncFailure, cli.ErrEnvRepoAccess) || errors.Is(syncFailure, cli.ErrEnvRepoUnavailable) {
			return syncFailure
		}
		return cli.NewInitIncompleteError("environment sync failed: " + syncFailure.Error())
	}
	if result.ActiveEnv == "" {
		return cli.NewInitIncompleteError("initialization did not select an environment")
	}
	if failure := doctorFailure(&daemon.DoctorResponse{Checks: result.Checks}, false); failure != nil {
		return failure
	}
	return cli.NewChecksFailedError("initialization saved settings, but required checks failed")
}

func initRecommendedActions(result initResult) []cli.JSONAction {
	if action, ok := initCheckRecoveryAction(result); ok {
		return []cli.JSONAction{action}
	}
	if initHasWorkingDirectoryFailure(result) {
		return []cli.JSONAction{}
	}
	completion := buildInitCompletion(result, nil)
	return []cli.JSONAction{{
		Command:     completion.JSONCommand,
		Reason:      completion.Reason,
		Destructive: false,
	}}
}

func initFailureRecommendedActions(result initResult, failure error) []cli.JSONAction {
	if errors.Is(failure, cli.ErrEnvRepoUnavailable) {
		return nil
	}
	return initRecommendedActions(result)
}

func buildInitCompletion(result initResult, syncFailure error) initCompletion {
	if errors.Is(syncFailure, cli.ErrEnvRepoUnavailable) {
		return initCompletion{
			Heading: "Setup is incomplete",
			Detail:  "Verify the environment repository URL first; if it is correct and private, authenticate Git.",
		}
	}
	if result.ActiveEnv == "" {
		return initCompletion{
			Heading:      "Setup is incomplete",
			Detail:       "No environment was selected. Fix the sync issue above, then retry setup.",
			HumanCommand: "orbit init",
			JSONCommand:  "orbit init --yes --json",
			Reason:       "Retry setup and select an environment.",
		}
	}
	if !result.Ready {
		if action, ok := initCheckRecoveryAction(result); ok {
			return initCompletion{
				Heading:      "Setup saved, but one prerequisite is missing",
				Detail:       "Apply the correction below, then Orbit can start the selected environment.",
				HumanCommand: strings.TrimSuffix(action.Command, " --json"),
				JSONCommand:  action.Command,
				Reason:       action.Reason,
			}
		}
		if initHasWorkingDirectoryFailure(result) {
			return initCompletion{
				Heading: "Setup saved, but one project path is unresolved",
				Detail:  "Set the path variable or update the service path shown above.",
			}
		}
		return initCompletion{
			Heading:      "Setup saved, but prerequisites are missing",
			Detail:       "Resolve the failed checks above before starting the environment.",
			HumanCommand: "orbit doctor",
			JSONCommand:  "orbit doctor --json",
			Reason:       "Verify the required tools after fixing the failed checks.",
		}
	}
	return initCompletion{
		Heading:      "Setup complete!",
		HumanCommand: "orbit up",
		JSONCommand:  "orbit up --json",
		Reason:       "Start the selected environment.",
		Ready:        true,
	}
}

func initCheckRecoveryAction(result initResult) (cli.JSONAction, bool) {
	if result.Ready || result.ActiveEnv == "" {
		return cli.JSONAction{}, false
	}
	actions := doctorRecommendedActions(&daemon.DoctorResponse{Checks: result.Checks})
	if len(actions) != 1 || actions[0].Command == "orbit status --json" {
		return cli.JSONAction{}, false
	}
	return actions[0], true
}

func initHasWorkingDirectoryFailure(result initResult) bool {
	for _, check := range result.Checks {
		if check.Status == daemon.CheckFail && strings.HasPrefix(check.Name, "Working directory (") {
			return true
		}
	}
	return false
}

// detectWorkspaceRoot tries to find the workspace root automatically.
func detectWorkspaceRoot(settings *daemon.Settings) string {
	cwd, _ := os.Getwd()
	if cwd != "" {
		if info, statErr := os.Stat(filepath.Join(cwd, "envs")); statErr == nil && info.IsDir() {
			return cwd
		}
	}

	if saved := settings.Get("workspace_root"); saved != "" {
		if _, err := os.Stat(saved); err == nil {
			return saved
		}
	}

	if envVal := daemon.WorkspaceRootFromEnv(); envVal != "" {
		if _, err := os.Stat(envVal); err == nil {
			return envVal
		}
	}

	home, _ := os.UserHomeDir()
	for _, c := range workspaceCandidates(home) {
		if len(workspaceMarkers(c)) > 0 {
			return c
		}
	}

	return ""
}

// workspaceCandidates aggregates the extensions' auto-detect candidate
// directories.
func workspaceCandidates(home string) []string {
	var out []string
	for _, ext := range extensions {
		if ext.CLIInit != nil && ext.CLIInit.WorkspaceCandidates != nil {
			out = append(out, ext.CLIInit.WorkspaceCandidates(home)...)
		}
	}
	return out
}

// workspaceMarkers aggregates the extensions' known-repo hits under root.
func workspaceMarkers(root string) []string {
	var found []string
	for _, ext := range extensions {
		if ext.CLIInit != nil && ext.CLIInit.WorkspaceMarkers != nil {
			found = append(found, ext.CLIInit.WorkspaceMarkers(root)...)
		}
	}
	return found
}

// listEnvFiles is a thin wrapper over the daemon server package's ListEnvYamls, preserved for
// local test and call-site ergonomics.
func listEnvFiles(envsDir string) []string {
	return daemonsrv.ListEnvYamls(envsDir)
}

// pickDefault selects the distribution's preferred env if available,
// else the first file.
func pickDefault(files []string) string {
	for _, f := range files {
		if f == distribution.DefaultEnv {
			return f
		}
	}
	if len(files) > 0 {
		return files[0]
	}
	return ""
}

func prompt(msg string) string {
	fmt.Print(msg)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// resolveInitEnvName matches the --env flag against available env files,
// accepting either a bare name ("example") or the full filename
// ("example.yaml"). Returns "" if no match.
func resolveInitEnvName(name string, available []string) string {
	target := name
	if !strings.HasSuffix(target, ".yaml") {
		target += ".yaml"
	}
	for _, f := range available {
		if f == target {
			return f
		}
	}
	return ""
}
