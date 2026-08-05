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
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsource"
	"github.com/iml885203/orbit/internal/envsync"
	"github.com/spf13/cobra"
)

var (
	initYes       bool
	initEnvRepo   string
	initEnvRef    string
	initEnvName   string
	initSource    string
	initPath      string
	initWorkspace string
)

type initResult struct {
	WorkspaceRoot string               `json:"workspace_root,omitempty"` // Deprecated: use source.workspace.
	Source        *envsource.Source    `json:"source,omitempty"`
	EnvsDir       string               `json:"envs_dir"`
	EnvRepoURL    string               `json:"env_repo_url,omitempty"`
	EnvRepoRef    string               `json:"env_repo_ref,omitempty"`
	EnvRepoCommit string               `json:"env_repo_commit,omitempty"`
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
	FollowUp     string
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

An existing project with orbit.yaml does not need init. Run orbit up anywhere
inside that project and Orbit discovers the nearest orbit.yaml automatically.

For non-interactive setup (CI, scripts):
  orbit init --yes                                         # accept all defaults
  orbit init --yes --source team --url <url>               # add a named Git source
  orbit init --yes --source team --url <url> --ref <ref>   # pin a branch, tag, or commit
  orbit init --yes --source local --path <dir> --env dev   # use a local source`,
		RunE: runInit,
	}
	cmd.Flags().BoolVarP(&initYes, "yes", "y", false, "accept defaults without prompting")
	cmd.Flags().StringVar(&initSource, "source", "", "name for the managed environment source")
	cmd.Flags().StringVar(&initEnvRepo, "url", "", "Git URL of the environment source")
	cmd.Flags().StringVar(&initPath, "path", "", "local directory containing envs/")
	cmd.Flags().StringVar(&initEnvRef, "ref", "", "repository branch, tag, or commit")
	cmd.Flags().StringVar(&initWorkspace, "workspace", "", "local application workspace for this source")
	cmd.Flags().String("env-repo", "", "deprecated; use --url")
	cmd.Flags().String("env-ref", "", "deprecated; use --ref")
	_ = cmd.Flags().MarkHidden("env-repo")
	_ = cmd.Flags().MarkHidden("env-ref")
	cmd.Flags().StringVar(&initEnvName, "env", "", "active env short name (e.g. dev); skips the selection prompt")
	return cmd
}

func runInit(cmd *cobra.Command, _ []string) error {
	if cmd != nil {
		if legacyURL, _ := cmd.Flags().GetString("env-repo"); initEnvRepo == "" {
			initEnvRepo = legacyURL
		}
		if legacyRef, _ := cmd.Flags().GetString("env-ref"); initEnvRef == "" {
			initEnvRef = legacyRef
		}
	}
	if initEnvRepo != "" && initPath != "" {
		return cli.NewInvalidArgumentError("--url and --path are mutually exclusive")
	}
	if initPath != "" && initEnvRef != "" {
		return cli.NewInvalidArgumentError("--ref is valid only with --url")
	}
	if (initEnvRepo != "" || initPath != "") && initSource == "" {
		if initYes || cli.JSONOutput {
			return cli.NewInvalidArgumentError("custom --url or --path requires --source")
		}
		initSource = prompt("  Source name: ")
		if initSource == "" {
			return cli.NewInvalidArgumentError("source name is required")
		}
	}
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
	configuredRegistry, err := sourceRegistry()
	if err != nil {
		return err
	}
	var configuredFirst *envsource.Source
	if initSource == "" && initEnvRepo == "" && initPath == "" && initEnvRef == "" {
		if existing, firstErr := configuredRegistry.First(); firstErr == nil {
			configuredFirst = &existing
		}
	}

	currentURL := settings.Get(settingKeyEnvRepoURL)
	currentRef := settings.Get(settingKeyEnvRepoRef)
	repository := resolveEnvRepository(initEnvRepo, initEnvRef, currentURL, currentRef)
	repoURL := repository.URL
	repoRef := repository.Ref
	// Accepting the release default must not copy it into settings.json;
	// otherwise upgrading Orbit could not advance its compatible demo ref.
	explicit := false
	switch {
	case initEnvRepo != "":
		repoURL = initEnvRepo
		explicit = true
	case repoURL == "" && !initYes:
		if input := prompt(fmt.Sprintf("  Git URL [%s]: ", repoURL)); input != "" {
			repoURL = input
			repoRef = initEnvRef
			explicit = true
		}
	}
	if initEnvRef != "" {
		repoRef = initEnvRef
		explicit = true
	}
	if configuredFirst != nil {
		repoURL = configuredFirst.URL
		repoRef = configuredFirst.Ref
		explicit = true
	}
	defaultQuickstart := !explicit &&
		currentURL == "" &&
		currentRef == "" &&
		distribution.DefaultEnv != "" &&
		repoURL == distribution.EnvRepoURL &&
		repoRef == distribution.EnvRepoRef
	if defaultQuickstart {
		output.boldln("Step 1: Quickstart")
	} else {
		output.boldln("Step 1: Environment source")
	}

	sourceName := initSource
	if sourceName == "" {
		sourceName = "default"
	}
	source := envsource.Source{Name: sourceName, Type: envsource.TypeGit, URL: repoURL, Ref: repoRef}
	if configuredFirst != nil {
		source = *configuredFirst
		sourceName = source.Name
	}
	if initPath != "" {
		normalized, err := envsource.ValidateLocalSource(initPath)
		if err != nil {
			return err
		}
		source.Type = envsource.TypeLocal
		source.URL = ""
		source.Ref = ""
		source.Path = normalized
	}
	if initWorkspace != "" {
		normalized, err := envsource.NormalizeExistingDirectory(initWorkspace)
		if err != nil {
			return err
		}
		source.Workspace = normalized
	}
	envsDir := envsource.EnvsDir(daemon.OrbitDir(), sourceName)
	if source.Location() == "" && len(sourceEnvironmentFiles(envsDestDir())) > 0 {
		envsDir = envsDestDir()
	}
	var syncFailure error
	var syncWarning string
	result.EnvsDir = envsDir
	if source.Location() == "" {
		warning := "no env repo configured; sync skipped"
		result.Warnings = append(result.Warnings, warning)
		output.warnln("  ! no env repo configured — skipping sync")
		output.faintln("    add one later with `orbit source add <name> --url <git-url>`")
	} else {
		result.EnvSource = source.Type
		result.EnvRepoURL = envsync.RedactURL(source.URL)
		result.EnvRepoRef = source.Ref
		if defaultQuickstart {
			output.println("  Preparing the Orbit demo")
		} else {
			sourceLabel := source.Location()
			if source.Type == envsource.TypeGit {
				sourceLabel = envsync.RedactURL(sourceLabel)
			}
			if source.Ref != "" {
				sourceLabel += " @ " + source.Ref
			}
			output.printf("  Syncing %s → %s\n", sourceLabel, envsDir)
		}
		registry, loadErr := envsource.Load(envsource.RegistryPath(daemon.OrbitDir()))
		if loadErr != nil {
			return loadErr
		}
		persist := false
		if _, getErr := registry.Get(source.Name); getErr == nil {
			persist = true
		}
		source, syncRes, err := envsource.Refresh(registry, source, daemon.OrbitDir(), false, persist)
		if err != nil {
			err = sourceSyncError(source, err)
			syncFailure = err
			syncWarning = "source sync failed: " + err.Error()
			if defaultQuickstart {
				output.warnln("  ! Could not prepare the Orbit demo")
			} else {
				output.warnln("  ! Could not sync the environment repository")
			}
			output.faintln("    Orbit will use an existing synced environment if one is available.")
		} else {
			result.SyncedFiles = syncRes.Written
			result.EnvRepoCommit = syncRes.Commit
			if !persist {
				if addErr := registry.Add(source); addErr != nil {
					_ = os.RemoveAll(envsource.SourceDir(daemon.OrbitDir(), source.Name))
					return addErr
				}
			}
			storedSource, getErr := registry.Get(source.Name)
			if getErr != nil {
				return getErr
			}
			result.Source = &storedSource
			if defaultQuickstart {
				output.printf("  %s Demo environment ready\n", cli.Green.Sprint("✓"))
			} else {
				output.printf("  %s synced %d file(s)\n", cli.Green.Sprint("✓"), len(syncRes.Written))
			}
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
		output.faintln("    retry `orbit source sync " + sourceName + "` once the source is reachable")
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
		_ = os.Setenv("ORBIT_SOURCE_NAME", source.Name)
		if source.Workspace != "" {
			_ = os.Setenv("WORKSPACE_ROOT", source.Workspace)
			result.WorkspaceRoot = source.Workspace
		} else {
			workspaceStepShown, result.WorkspaceRoot, err = configureRequiredWorkspace(output, settings, absPath)
		}
		if err != nil {
			return err
		}
		if result.WorkspaceRoot != source.Workspace {
			source.Workspace = result.WorkspaceRoot
			registry, loadErr := envsource.Load(envsource.RegistryPath(daemon.OrbitDir()))
			if loadErr != nil {
				return loadErr
			}
			if replaceErr := registry.Replace(source); replaceErr != nil {
				return replaceErr
			}
		}
		if err := settings.ClearLegacyEnvironmentSettings(); err != nil {
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
) (bool, string, error) {
	contents, err := os.ReadFile(envPath)
	if err != nil {
		return false, "", err
	}
	if !requiresWorkspaceRoot(contents) {
		return false, "", nil
	}

	candidate := detectWorkspaceRoot(settings)
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
	output.printf("  Environment %s needs local project files.\n", daemonsrv.EnvShortName(envPath))
	root := candidate
	if !initYes {
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
	if err := os.Setenv("WORKSPACE_ROOT", root); err != nil {
		return false, "", fmt.Errorf("applying workspace root: %w", err)
	}
	output.printf("  %s Project workspace: %s\n", cli.Green.Sprint("✓"), root)
	return true, root, nil
}

func requiresWorkspaceRoot(contents []byte) bool {
	for _, line := range strings.Split(string(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if comment := strings.Index(trimmed, " #"); comment >= 0 {
			trimmed = trimmed[:comment]
		}
		if strings.Contains(trimmed, "${WORKSPACE_ROOT}") {
			return true
		}
	}
	return false
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
		if completion.Reason == "" || completion.FollowUp != "" {
			output.faintln("  Next: " + completion.HumanCommand)
		} else {
			output.faintln(fmt.Sprintf("  Next: %-16s %s", completion.HumanCommand, completion.Reason))
		}
	}
	if completion.FollowUp != "" {
		output.faintln("  Then: " + completion.FollowUp)
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
		if check, ok := initExternalPrerequisite(result); ok {
			return initCompletion{
				Heading:      "Setup saved — one prerequisite remains",
				Detail:       check.Message,
				HumanCommand: check.Hint,
				FollowUp:     "orbit up",
				JSONCommand:  "orbit doctor --json",
				Reason:       "Verify the selected environment after installing the missing prerequisite.",
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

func initExternalPrerequisite(result initResult) (daemon.DoctorCheck, bool) {
	var failed []daemon.DoctorCheck
	for _, check := range result.Checks {
		if check.Status == daemon.CheckFail && check.Name != "Daemon" {
			failed = append(failed, check)
		}
	}
	if len(failed) != 1 || failed[0].Hint == "" || strings.HasPrefix(failed[0].Hint, "run: ") {
		return daemon.DoctorCheck{}, false
	}
	return failed[0], true
}

// detectWorkspaceRoot tries to find the workspace root automatically.
func detectWorkspaceRoot(settings *daemon.Settings) string {
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
