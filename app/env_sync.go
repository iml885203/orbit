package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	envSyncURL     string
	envSyncRef     string
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
	cmd.Flags().StringVar(&envSyncRef, "ref", "", "pin and remember a repository branch, tag, or commit")
	cmd.Flags().StringVar(&envSyncPath, "path", "", "use a local checkout containing envs/ without remembering it")
	cmd.Flags().BoolVar(&envSyncDryRun, "dry", false, "preview updates without downloading or applying")
	cmd.Flags().BoolVarP(&envSyncYes, "yes", "y", false, "confirm applying updates without prompting")
	cmd.Flags().BoolVar(&envSyncNoApply, "no-apply", false, "download now and defer applying active updates")
	return cmd
}

func runEnvSync(_ *cobra.Command, _ []string) error {
	action := cli.JSONAction{Command: "orbit source sync --json", Reason: "Synchronize the default environment source.", Destructive: false}
	message := "orbit env sync has moved to orbit source sync"
	switch {
	case envSyncURL != "":
		action = cli.JSONAction{Command: "orbit source add <name> --url " + envSyncURL + " --json", Reason: "Add the Git repository as a named source.", Destructive: false}
		message = "orbit env sync --url no longer changes sources; use orbit source add <name> --url " + envSyncURL
	case envSyncPath != "":
		action = cli.JSONAction{Command: "orbit source add <name> --path " + envSyncPath + " --json", Reason: "Add the local directory as a persistent named source.", Destructive: false}
		message = "orbit env sync --path no longer changes sources; use orbit source add <name> --path " + envSyncPath
	case envSyncRef != "":
		action = cli.JSONAction{Command: "orbit source update <name> --ref " + envSyncRef + " --json", Reason: "Update the ref of a named Git source.", Destructive: false}
		message = "orbit env sync --ref no longer changes sources; use orbit source update <name> --ref " + envSyncRef
	}
	if cli.JSONOutput {
		return cli.WithJSONReplacementActions(errors.New(message), []cli.JSONAction{action})
	}
	fmt.Println(message)
	return nil
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
	Reference     string   `json:"reference,omitempty"`
	Commit        string   `json:"commit,omitempty"`
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
		Reference:     opts.Result.Source.Ref,
		Commit:        opts.Result.Source.Commit,
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
