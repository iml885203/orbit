package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsync"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

const (
	settingKeyEnvRepoURL = "env_repo_url"
	settingKeyEnvRepoRef = "env_repo_ref"
)

var (
	envSyncDryRun  bool
	envSyncYes     bool
	envSyncNoApply bool
)

func envSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Deprecated compatibility bridge to orbit source sync",
		Long: `Deprecated: shared environments are now managed with orbit source.

With no repository-changing flags, this command synchronizes the first source
exactly like "orbit source sync" and prints a deprecation warning. --dry, --yes,
and --no-apply retain their synchronization behavior.

Repository-changing legacy forms no longer mutate source configuration. Orbit
rejects them without making changes. Remove and add a source when its location
needs to change.

Old to new:
  orbit env sync                         -> orbit source sync
  orbit env sync --dry                   -> orbit source sync --dry
  orbit env sync --url URL               -> orbit source remove NAME; orbit source add NAME --url URL
  orbit env sync --path PATH             -> orbit source remove NAME; orbit source add NAME --path PATH

Run "orbit source list" to find NAME. Use "orbit source add" when the source
does not exist.`,
		RunE: runEnvSync,
	}
	cmd.Flags().String("url", "", "deprecated; remove and re-add the source with the new URL")
	cmd.Flags().String("ref", "", "deprecated; remove and re-add the source with the desired ref")
	cmd.Flags().String("path", "", "deprecated; remove and re-add the source with the new path")
	cmd.Flags().BoolVar(&envSyncDryRun, "dry", false, "preview updates without downloading or applying")
	cmd.Flags().BoolVarP(&envSyncYes, "yes", "y", false, "confirm applying updates without prompting")
	cmd.Flags().BoolVar(&envSyncNoApply, "no-apply", false, "download now and defer applying active updates")
	return cmd
}

func runEnvSync(cmd *cobra.Command, _ []string) error {
	urlChanged := cmd.Flags().Changed("url")
	pathChanged := cmd.Flags().Changed("path")
	refChanged := cmd.Flags().Changed("ref")
	if urlChanged || pathChanged || refChanged {
		return cli.WithJSONReplacementActions(
			cli.NewInvalidArgumentError("legacy repository flags no longer configure env sync; use 'orbit source' to add or replace a source"),
			[]cli.JSONAction{{Command: "orbit source --help", Reason: "Choose the source workflow for the desired location."}},
		)
	}
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning: 'orbit env sync' is deprecated; use 'orbit source sync'.")
	syncCommand := sourceSyncCmdWithDeprecation("orbit env sync")
	for name, value := range map[string]bool{
		"dry": envSyncDryRun, "yes": envSyncYes, "no-apply": envSyncNoApply,
	} {
		if value {
			if err := syncCommand.Flags().Set(name, strconv.FormatBool(value)); err != nil {
				return err
			}
		}
	}
	return syncCommand.RunE(syncCommand, nil)
}

func envRepoSyncError(err error) error {
	var cloneErr *envsync.CloneError
	if !errors.As(err, &cloneErr) {
		return fmt.Errorf("sync: %w", err)
	}
	if errors.Is(cloneErr.Err, exec.ErrNotFound) {
		return cli.WithJSONActions(
			cli.NewEnvRepoAccessError(
				"Git is required only to sync shared environments, but git was not found on PATH.\n"+
					"Install Git from https://git-scm.com/downloads, then retry 'orbit source sync'.",
			),
			[]cli.JSONAction{{
				Command:     "orbit source sync --json",
				Reason:      "Retry the environment sync after installing Git.",
				Destructive: false,
			}},
		)
	}
	message := fmt.Sprintf("cannot access environment repo %s: %v", cloneErr.DisplayURL(), cloneErr.Err)
	if detail := strings.TrimSpace(strings.ReplaceAll(cloneErr.Output, cloneErr.URL, cloneErr.DisplayURL())); detail != "" {
		message += "\nGit: " + detail
	}
	if cloneErr.ReportsAmbiguousGitHubAvailability() {
		message += "\nCheck the GitHub owner and repository name first. GitHub uses the same response when a private repository is hidden from your current credentials."
		message += "\nIf the URL is correct and the repo is private, authenticate Git; otherwise update the named source URL."
		return cli.NewEnvRepoUnavailableError(message)
	}
	if cloneErr.IsGitHub() {
		message += "\nFor a private GitHub repo, run 'gh auth login' and 'gh auth setup-git', then retry 'orbit source sync'."
		return cli.WithJSONActions(cli.NewEnvRepoAccessError(message), []cli.JSONAction{
			{Command: "gh auth login", Reason: "Authenticate GitHub CLI for private repository access.", Destructive: false},
			{Command: "gh auth setup-git", Reason: "Configure Git to use the authenticated GitHub CLI credentials.", Destructive: false},
			{Command: "orbit source sync --json", Reason: "Retry the environment source sync after restoring Git access.", Destructive: false},
		})
	} else {
		message += "\nVerify the repository URL and Git access, then retry 'orbit source sync'."
	}
	return cli.NewEnvRepoAccessError(message)
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

type envRepository struct {
	URL string
	Ref string
}

func resolveEnvRepository(flagURL, flagRef, settingURL, settingRef string) envRepository {
	if flagURL != "" {
		return envRepository{URL: flagURL, Ref: flagRef}
	}
	if settingURL != "" {
		ref := settingRef
		if flagRef != "" {
			ref = flagRef
		}
		return envRepository{URL: settingURL, Ref: ref}
	}
	if envURL := os.Getenv("ORBIT_ENV_REPO_URL"); envURL != "" {
		ref := os.Getenv("ORBIT_ENV_REPO_REF")
		if flagRef != "" {
			ref = flagRef
		}
		return envRepository{URL: envURL, Ref: ref}
	}
	ref := distribution.EnvRepoRef
	if flagRef != "" {
		ref = flagRef
	}
	return envRepository{URL: distribution.EnvRepoURL, Ref: ref}
}

// envsDestDir returns the local destination for synced envs.
func envsDestDir() string {
	return daemonsrv.DefaultEnvsDir()
}
