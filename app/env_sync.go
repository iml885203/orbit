package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsync"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

const settingKeyEnvRepoURL = "env_repo_url"

var (
	envSyncURL     string
	envSyncPath    string
	envSyncDryRun  bool
	envSyncYes     bool
	envSyncNoApply bool
)

func envSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Refresh shared environment configuration",
		Long: `Refresh shared environment configuration and make active updates current.

If the running environment changed, Orbit offers to apply the update and
restores the resources that were running. Use --yes to apply non-interactively,
or --no-apply when an interruption must be deferred.

By default, Orbit uses the source selected during init. Maintainers can use
--url to select and remember another Git repository, or --path to test the
envs/ directory in a local checkout, including uncommitted changes. A file://
URL still performs a Git clone and therefore includes only committed changes.

Use --dry to preview changed files without downloading or applying them.`,
		RunE: runEnvSync,
	}
	cmd.Flags().StringVar(&envSyncURL, "url", "", "use and remember another environment Git repository")
	cmd.Flags().StringVar(&envSyncPath, "path", "", "use a local checkout containing envs/ without remembering it")
	cmd.Flags().BoolVar(&envSyncDryRun, "dry", false, "preview updates without downloading or applying")
	cmd.Flags().BoolVar(&envSyncYes, "yes", false, "apply updates without prompting")
	cmd.Flags().BoolVar(&envSyncNoApply, "no-apply", false, "download now and defer applying active updates")
	cmd.Flags().BoolVar(&envSyncNoApply, "no-restart", false, "deprecated alias for --no-apply")
	_ = cmd.Flags().MarkHidden("no-restart")
	return cmd
}

func runEnvSync(_ *cobra.Command, _ []string) error {
	if envSyncURL != "" && envSyncPath != "" {
		return fmt.Errorf("--url and --path are mutually exclusive")
	}

	dest := envsDestDir()

	if envSyncPath != "" {
		envsDir := filepath.Join(envSyncPath, "envs")
		if !cli.JSONOutput {
			fmt.Printf("Checking for environment updates from %s...\n", envSyncPath)
		}
		res, err := envsync.Sync(envsDir, dest, envsync.Options{DryRun: envSyncDryRun})
		if err != nil {
			return fmt.Errorf("sync: %w", err)
		}
		if cli.JSONOutput {
			return finishEnvSync(envSyncPath, dest, res)
		}
		printSyncResult(res)
		return offerEnvironmentApply()
	}

	settings := daemon.LoadSettings(daemon.DefaultSettingsPath())
	priorURL := settings.Get(settingKeyEnvRepoURL)
	url := resolveEnvRepoURL(envSyncURL, priorURL)
	if url == "" {
		return fmt.Errorf("no env repo URL configured; pass --url, --path, or run `orbit init`")
	}
	if !cli.JSONOutput {
		fmt.Println("Checking for environment updates...")
	}
	res, err := envsync.SyncFromRepo(url, dest, envsync.Options{DryRun: envSyncDryRun})
	if err != nil {
		return envRepoSyncError(err)
	}
	if !cli.JSONOutput {
		printSyncResult(res)
	}

	// Persist only an explicit --url. A URL that resolved from the built-in
	// default must not be written back: pinning it would freeze the default
	// into settings.json, and users who never overrode would stop following
	// default changes shipped in newer releases.
	if envSyncURL != "" {
		if err := settings.Set(settingKeyEnvRepoURL, url); err != nil {
			return fmt.Errorf("saving settings: %w", err)
		}
	}
	if cli.JSONOutput {
		return finishEnvSync(url, dest, res)
	}
	return offerEnvironmentApply()
}

func envRepoSyncError(err error) error {
	var cloneErr *envsync.CloneError
	if !errors.As(err, &cloneErr) {
		return fmt.Errorf("sync: %w", err)
	}
	message := fmt.Sprintf("cannot access environment repo %s: %v", cloneErr.DisplayURL(), cloneErr.Err)
	if detail := strings.TrimSpace(strings.ReplaceAll(cloneErr.Output, cloneErr.URL, cloneErr.DisplayURL())); detail != "" {
		message += "\nGit: " + detail
	}
	if cloneErr.ReportsAmbiguousGitHubAvailability() {
		message += "\nCheck the GitHub owner and repository name first. GitHub uses the same response when a private repository is hidden from your current credentials."
		message += "\nIf the URL is correct and the repo is private, authenticate Git; otherwise retry with 'orbit init --env-repo <correct-url>' or 'orbit env sync --url <correct-url>'."
		return cli.NewEnvRepoUnavailableError(message)
	}
	if cloneErr.IsGitHub() {
		message += "\nFor a private GitHub repo, run 'gh auth login' and 'gh auth setup-git', then retry 'orbit env sync'."
		return cli.WithJSONActions(cli.NewEnvRepoAccessError(message), []cli.JSONAction{
			{Command: "gh auth login", Reason: "Authenticate GitHub CLI for private repository access.", Destructive: false},
			{Command: "gh auth setup-git", Reason: "Configure Git to use the authenticated GitHub CLI credentials.", Destructive: false},
			{Command: "orbit env sync --json", Reason: "Retry the environment sync after restoring Git access.", Destructive: false},
		})
	} else {
		message += "\nVerify the repository URL and Git access, then retry 'orbit env sync'."
	}
	return cli.NewEnvRepoAccessError(message)
}

type envSyncJSONOptions struct {
	Source        string
	Destination   string
	DryRun        bool
	Result        envsync.Result
	DaemonRunning bool
	ApplyAction   string
	ApplyResult   *environmentApplyResult
}

type envSyncJSONData struct {
	Operation     string   `json:"operation"`
	Source        string   `json:"source"`
	Destination   string   `json:"destination"`
	DryRun        bool     `json:"dry_run"`
	Written       []string `json:"written"`
	DaemonRunning bool     `json:"daemon_running"`
	ApplyAction   string   `json:"apply_action"`
	Restored      []string `json:"restored_resources"`
}

func buildEnvSyncJSONData(opts envSyncJSONOptions) envSyncJSONData {
	written := []string{}
	if opts.Result.Written != nil {
		written = opts.Result.Written
	}
	restored := []string{}
	if opts.ApplyResult != nil {
		restored = opts.ApplyResult.RestoredResources
	}
	return envSyncJSONData{
		Operation:     "env_sync",
		Source:        opts.Source,
		Destination:   opts.Destination,
		DryRun:        opts.DryRun,
		Written:       written,
		DaemonRunning: opts.DaemonRunning,
		ApplyAction:   opts.ApplyAction,
		Restored:      restored,
	}
}

func envSyncApplyAction(changesPending, daemonRunning, dryRun, noApply, applied bool) string {
	if applied {
		return "applied"
	}
	if dryRun || !changesPending || !daemonRunning {
		return "none"
	}
	if noApply {
		return "deferred"
	}
	return "recommended"
}

func envSyncRecommendedActions(applyAction string) []cli.JSONAction {
	if applyAction != "recommended" {
		return nil
	}
	return []cli.JSONAction{{
		Command:     "orbit env apply --json",
		Reason:      "Apply downloaded environment updates and restore running resources.",
		Destructive: false,
	}}
}

func finishEnvSync(source, destination string, syncResult envsync.Result) error {
	applyResult, err := inspectEnvironmentApply()
	if err != nil {
		return err
	}
	if envSyncYes && !envSyncDryRun && !envSyncNoApply && applyResult.ChangesPending {
		applied, applyErr := applyEnvironmentChanges(nil)
		if applyErr != nil {
			return applyErr
		}
		applyResult = applied
	}
	action := envSyncApplyAction(
		applyResult.ChangesPending,
		applyResult.DaemonRunning,
		envSyncDryRun,
		envSyncNoApply,
		applyResult.Applied,
	)
	return cli.WriteJSONSuccess(os.Stdout, commandString(), buildEnvSyncJSONData(envSyncJSONOptions{
		Source:        source,
		Destination:   destination,
		DryRun:        envSyncDryRun,
		Result:        syncResult,
		DaemonRunning: applyResult.DaemonRunning,
		ApplyAction:   action,
		ApplyResult:   &applyResult,
	}), envSyncRecommendedActions(action))
}

func inspectEnvironmentApply() (environmentApplyResult, error) {
	result := emptyEnvironmentApplyResult()
	pid, alive := daemon.IsDaemonRunning()
	result.DaemonRunning = alive
	result.PreviousPID = pid
	result.PID = pid
	if !alive {
		return result, nil
	}
	status, err := daemon.NewClient(daemon.DefaultSocketPath()).Status()
	if err != nil {
		return result, fmt.Errorf("checking environment changes: %w", err)
	}
	result.ChangesPending = status.ConfigStale
	result.FinalStatus = status
	return result, nil
}

func offerEnvironmentApply() error {
	if envSyncDryRun {
		return nil
	}
	pending, err := inspectEnvironmentApply()
	if err != nil {
		return err
	}
	if !pending.DaemonRunning || !pending.ChangesPending {
		return nil
	}
	if envSyncNoApply {
		fmt.Println("\nUpdates downloaded. Apply later with: orbit env apply")
		return nil
	}

	if !envSyncYes {
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			fmt.Println("\nEnvironment updates downloaded. Apply when ready with: orbit env apply")
			return nil
		}
		message := "\nApply environment updates now?"
		running := runningEnvironmentResources(pending.FinalStatus.Resources)
		if len(running) > 0 {
			message = fmt.Sprintf("\nApply environment updates and restore %d running resource(s)?", len(running))
		}
		if !cli.Confirm(message) {
			fmt.Println("Updates downloaded. Apply later with: orbit env apply")
			return nil
		}
	}

	result, err := applyEnvironmentChanges(environmentApplyProgress())
	if err != nil {
		return err
	}
	printEnvironmentApplyResult(result)
	return nil
}

func printSyncResult(res envsync.Result) {
	if len(res.Written) == 0 {
		fmt.Println("Environment is up to date.")
		return
	}
	message := "Updated"
	if envSyncDryRun {
		message = "Would update"
	}
	fmt.Printf("%s %d environment file(s):\n", message, len(res.Written))
	for _, f := range res.Written {
		fmt.Printf("  %s\n", f)
	}
}

// resolveEnvRepoURL picks flag > setting > ORBIT_ENV_REPO_URL env > the
// distribution default ("" in an unbranded build — callers report the
// missing configuration).
func resolveEnvRepoURL(flag, setting string) string {
	if flag != "" {
		return flag
	}
	if setting != "" {
		return setting
	}
	if envURL := os.Getenv("ORBIT_ENV_REPO_URL"); envURL != "" {
		return envURL
	}
	return distribution.EnvRepoURL
}

// envsDestDir returns the local destination for synced envs.
func envsDestDir() string {
	return daemonsrv.DefaultEnvsDir()
}
