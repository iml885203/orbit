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
	envSyncURL       string
	envSyncPath      string
	envSyncDryRun    bool
	envSyncYes       bool
	envSyncNoRestart bool
)

func envSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync env configs from the remote env repo",
		Long: `Clone the configured env repo and copy its envs/ tree into ~/.orbit/envs/.

The repo URL is resolved in order: --url flag, settings key "env_repo_url",
ORBIT_ENV_REPO_URL env var, then the build's distribution default (if the
binary ships one). Use --url to override and persist a new URL; use --dry
to preview without writing.

The URL accepts any git-clonable form, including a local path via the
file:// scheme (e.g. file:///Users/me/dev/orbit). Note that file:// still
performs a git clone, so only committed changes are synced.

To sync from a local working tree (including uncommitted changes), use
--path pointing at a directory that contains an envs/ subdirectory.
This bypasses git entirely and copies files directly from disk.`,
		RunE: runEnvSync,
	}
	cmd.Flags().StringVar(&envSyncURL, "url", "", "git URL of the env repo (overrides and persists)")
	cmd.Flags().StringVar(&envSyncPath, "path", "", "local directory containing envs/ (bypasses git; not persisted)")
	cmd.Flags().BoolVar(&envSyncDryRun, "dry", false, "preview without writing")
	cmd.Flags().BoolVar(&envSyncYes, "yes", false, "skip restart confirmation prompt")
	cmd.Flags().BoolVar(&envSyncNoRestart, "no-restart", false, "do not offer or perform daemon restart")
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
			fmt.Printf("Syncing from %s (local path)\n  → %s\n", envSyncPath, dest)
		}
		res, err := envsync.Sync(envsDir, dest, envsync.Options{DryRun: envSyncDryRun})
		if err != nil {
			return fmt.Errorf("sync: %w", err)
		}
		if cli.JSONOutput {
			_, alive := daemon.IsDaemonRunning()
			return cli.WriteJSONSuccess(os.Stdout, commandString(), buildEnvSyncJSONData(envSyncJSONOptions{
				Source:        envSyncPath,
				Destination:   dest,
				DryRun:        envSyncDryRun,
				Result:        res,
				DaemonRunning: alive,
				RestartAction: envSyncRestartAction(len(res.Written) > 0, alive, envSyncDryRun, envSyncNoRestart),
			}), envSyncRecommendedActions(len(res.Written) > 0, alive, envSyncDryRun, envSyncNoRestart))
		}
		printSyncResult(res)
		return offerDaemonRestart(len(res.Written) > 0)
	}

	settings := daemon.LoadSettings(daemon.DefaultSettingsPath())
	priorURL := settings.Get(settingKeyEnvRepoURL)
	url := resolveEnvRepoURL(envSyncURL, priorURL)
	if url == "" {
		return fmt.Errorf("no env repo URL configured; pass --url, --path, or run `orbit init`")
	}
	if !cli.JSONOutput {
		fmt.Printf("Syncing from %s\n  → %s\n", url, dest)
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
		_, alive := daemon.IsDaemonRunning()
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildEnvSyncJSONData(envSyncJSONOptions{
			Source:        url,
			Destination:   dest,
			DryRun:        envSyncDryRun,
			Result:        res,
			DaemonRunning: alive,
			RestartAction: envSyncRestartAction(len(res.Written) > 0, alive, envSyncDryRun, envSyncNoRestart),
		}), envSyncRecommendedActions(len(res.Written) > 0, alive, envSyncDryRun, envSyncNoRestart))
	}
	return offerDaemonRestart(len(res.Written) > 0)
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
	RestartAction string
}

type envSyncJSONData struct {
	Operation     string   `json:"operation"`
	Source        string   `json:"source"`
	Destination   string   `json:"destination"`
	DryRun        bool     `json:"dry_run"`
	Written       []string `json:"written"`
	DaemonRunning bool     `json:"daemon_running"`
	RestartAction string   `json:"restart_action"`
}

func buildEnvSyncJSONData(opts envSyncJSONOptions) envSyncJSONData {
	written := []string{}
	if opts.Result.Written != nil {
		written = opts.Result.Written
	}
	return envSyncJSONData{
		Operation:     "env_sync",
		Source:        opts.Source,
		Destination:   opts.Destination,
		DryRun:        opts.DryRun,
		Written:       written,
		DaemonRunning: opts.DaemonRunning,
		RestartAction: opts.RestartAction,
	}
}

func envSyncRestartAction(filesChanged, daemonRunning, dryRun, noRestart bool) string {
	if dryRun || noRestart || !filesChanged || !daemonRunning {
		return "none"
	}
	return "recommended"
}

func envSyncRecommendedActions(filesChanged, daemonRunning, dryRun, noRestart bool) []cli.JSONAction {
	if envSyncRestartAction(filesChanged, daemonRunning, dryRun, noRestart) != "recommended" {
		return nil
	}
	return []cli.JSONAction{{
		Command:     "orbit daemon restart --json",
		Reason:      "Apply synced environment files to the running daemon.",
		Destructive: false,
	}}
}

// offerDaemonRestart prompts the user to restart the daemon when env files
// changed and a daemon is currently running. The prompt is skipped (and
// restart performed) when --yes is set; skipped entirely when --no-restart
// is set, when no files changed, or when no daemon is running. Dry runs
// always skip — nothing was actually written.
func offerDaemonRestart(filesChanged bool) error {
	if envSyncDryRun || envSyncNoRestart || !filesChanged {
		return nil
	}
	if _, alive := daemon.IsDaemonRunning(); !alive {
		return nil
	}

	if !envSyncYes {
		// No TTY → no chance of asking; print the same hint as `orbit switch`
		// gives and let the operator decide. Avoids blocking CI / scripted
		// runs while still nagging interactive users.
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			fmt.Println("\n⚠ Daemon is running with the previous env. Apply with: orbit daemon restart")
			return nil
		}
		if !cli.Confirm("\nDaemon is running with the previous env. Restart now?") {
			fmt.Println("Skipped. Apply later with: orbit daemon restart")
			return nil
		}
	}

	fmt.Println("→ orbit daemon restart")
	return runDaemonRestart(nil, nil)
}

func printSyncResult(res envsync.Result) {
	if len(res.Written) == 0 {
		fmt.Println("Already up to date.")
		return
	}
	verb := "wrote"
	if envSyncDryRun {
		verb = "would write"
	}
	fmt.Printf("%s %d file(s):\n", verb, len(res.Written))
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
