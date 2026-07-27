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
	"github.com/iml885203/orbit/internal/envsync"
	"github.com/spf13/cobra"
)

var (
	initYes     bool
	initEnvRepo string
	initEnvName string
)

type initResult struct {
	WorkspaceRoot string               `json:"workspace_root"`
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
		Long: `Interactive wizard that detects your workspace, selects an environment, and verifies your setup.

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

	output.boldln("Step 1: Workspace root")

	root := detectWorkspaceRoot(settings)
	if root != "" {
		if !initYes {
			output.printf("  Found: %s\n", root)
			input := prompt(fmt.Sprintf("  Workspace root [%s]: ", root))
			if input != "" {
				root = expandHome(input)
			}
		}
	} else {
		if initYes {
			return fmt.Errorf("could not auto-detect the workspace root — run without --yes")
		}
		output.faintln("  Could not auto-detect the workspace root (the directory holding your repo checkouts).")
		root = expandHome(prompt(fmt.Sprintf("  Enter path (e.g. %s): ", workspaceExample())))
	}

	if root == "" {
		return fmt.Errorf("workspace root is required")
	}
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("path not found: %s", root)
	}

	// Validate — check for known repos (markers come from the extensions;
	// an unbranded build accepts any existing directory silently).
	markers := workspaceMarkers(root)
	if len(markers) > 0 {
		output.printf("  %s workspace root %s (contains %s)\n", cli.Green.Sprint("✓"), root, strings.Join(markers, ", "))
	} else if hint := workspaceMarkerHint(); hint != "" {
		output.printf("  %s workspace root %s (no known repos found — %s)\n", cli.Yellow.Sprint("!"), root, hint)
	} else {
		output.printf("  %s workspace root %s\n", cli.Green.Sprint("✓"), root)
	}
	result.WorkspaceRoot = root

	if err := settings.Set("workspace_root", root); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	settings.ApplyToEnv()

	// Feature-specific settings steps (each extension's own keys and
	// prompts) come from the registered extensions — the wizard owns the
	// flow, the extension owns its steps.
	for _, ext := range extensions {
		if ext.CLIInit != nil && ext.CLIInit.Steps != nil {
			if err := ext.CLIInit.Steps(settings, initYes, prompt, cli.JSONOutput); err != nil {
				return err
			}
		}
	}

	output.println()
	output.boldln("Step 2: Env repo")

	currentURL := settings.Get(settingKeyEnvRepoURL)
	repoURL := currentURL
	if repoURL == "" {
		repoURL = distribution.EnvRepoURL
	}
	// Persist only what the user explicitly typed (or passed via flag).
	// Accepting the suggested default with Enter must not pin it into
	// settings.json — an unpinned default keeps following new releases.
	explicit := false
	switch {
	case initEnvRepo != "":
		repoURL = initEnvRepo
		explicit = true
	case !initYes:
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
	result.EnvsDir = envsDir
	result.EnvRepoURL = repoURL
	localEnvsDir := filepath.Join(root, "envs")
	if info, err := os.Stat(localEnvsDir); err == nil && info.IsDir() && initEnvRepo == "" {
		result.EnvSource = "local"
		output.printf("  Syncing local %s → %s\n", localEnvsDir, envsDir)
		syncRes, err := envsync.Sync(localEnvsDir, envsDir, envsync.Options{})
		if err != nil {
			syncFailure = err
			warning := "env sync failed: " + err.Error()
			result.Warnings = append(result.Warnings, warning)
			output.warnf("  ! sync failed: %v\n", err)
			output.faintln("    you can retry later with `orbit env sync`")
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
			warning := "env sync failed: " + err.Error()
			result.Warnings = append(result.Warnings, warning)
			output.warnf("  ! sync failed: %v\n", err)
			output.faintln("    you can retry later with `orbit env sync`")
		} else {
			result.SyncedFiles = syncRes.Written
			output.printf("  %s synced %d file(s)\n", cli.Green.Sprint("✓"), len(syncRes.Written))
		}
	}

	output.println()
	output.boldln("Step 3: Environment")

	envFiles := listEnvFiles(envsDir)
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
			output.printf("  %s Environment: %s\n", cli.Green.Sprint("✓"), chosen)
		case initYes || len(envFiles) == 1:
			chosen = pickDefault(envFiles)
			output.printf("  %s Environment: %s\n", cli.Green.Sprint("✓"), chosen)
		default:
			for i, f := range envFiles {
				output.printf("  %d) %s\n", i+1, f)
			}
			input := prompt("  Select environment [1]: ")
			idx := 0
			if input != "" {
				if n, err := strconv.Atoi(input); err == nil && n >= 1 && n <= len(envFiles) {
					idx = n - 1
				}
			}
			chosen = envFiles[idx]
			output.printf("  %s Environment: %s\n", cli.Green.Sprint("✓"), chosen)
		}

		absPath := filepath.Join(envsDir, chosen)
		if err := writeCurrentEnv(absPath); err != nil {
			return fmt.Errorf("writing env: %w", err)
		}
		configFile = absPath
		result.ActiveEnv = chosen
		result.ConfigPath = absPath
	}

	output.println()
	output.boldln("Step 4: Health check")
	if output.enabled {
		_ = runDoctorWithOptions(doctorOptions{})
	}
	health := doctorResponse(daemon.NewClient(daemon.DefaultSocketPath()))
	result.Checks = health.Checks
	result.Ready = result.ActiveEnv != "" && doctorFailure(health, false) == nil

	output.println()
	completion := buildInitCompletion(result)
	if completion.Ready {
		output.boldln(completion.Heading)
	} else {
		output.warnln(completion.Heading)
	}
	if completion.Detail != "" {
		output.faintln("  " + completion.Detail)
	}
	output.faintln(fmt.Sprintf("  Next: %-16s %s", completion.HumanCommand, completion.Reason))

	if cli.JSONOutput {
		if !result.Ready {
			failure := initFailure(result, syncFailure)
			if err := cli.WriteJSONFailure(os.Stdout, commandString(), result, failure, initRecommendedActions(result)); err != nil {
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
		if errors.Is(syncFailure, cli.ErrEnvRepoAccess) {
			return syncFailure
		}
		return cli.NewInitIncompleteError("environment sync failed: " + syncFailure.Error())
	}
	if result.ActiveEnv == "" {
		return cli.NewInitIncompleteError("initialization did not select an environment")
	}
	return cli.NewChecksFailedError("initialization saved settings, but required checks failed")
}

func initRecommendedActions(result initResult) []cli.JSONAction {
	completion := buildInitCompletion(result)
	return []cli.JSONAction{{
		Command:     completion.JSONCommand,
		Reason:      completion.Reason,
		Destructive: false,
	}}
}

func buildInitCompletion(result initResult) initCompletion {
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

	// A clean checkout has no Orbit markers yet. Falling back to where the
	// user invoked init keeps first use aligned with every other project CLI:
	// the current directory is the workspace unless they say otherwise.
	return cwd
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

// workspaceExample renders the prompt's example path: the first
// extension candidate (home contracted back to ~), or a generic
// placeholder for an unbranded build.
func workspaceExample() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/dev/workspace"
	}
	if cands := workspaceCandidates(home); len(cands) > 0 {
		if home != "" && strings.HasPrefix(cands[0], home) {
			return "~" + cands[0][len(home):]
		}
		return cands[0]
	}
	return "~/dev/workspace"
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

// workspaceMarkerHint returns the first extension's description of what
// the markers are, for the none-found warning.
func workspaceMarkerHint() string {
	for _, ext := range extensions {
		if ext.CLIInit != nil && ext.CLIInit.MarkerHint != "" {
			return ext.CLIInit.MarkerHint
		}
	}
	return ""
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
