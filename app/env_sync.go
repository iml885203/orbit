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
	"github.com/iml885203/orbit/internal/envsource"
	"github.com/iml885203/orbit/internal/envsync"
	"github.com/iml885203/orbit/internal/shellquote"
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
		Short: "Deprecated compatibility bridge to orbit source sync",
		Long: `Deprecated: shared environments are now managed with orbit source.

With no repository-changing flags, this command synchronizes the default source
exactly like "orbit source sync" and prints a deprecation warning. --dry, --yes,
and --no-apply retain their synchronization behavior.

Repository-changing legacy forms no longer mutate source configuration. Orbit
rejects them without making changes and provides a concrete orbit source command.

Old to new:
  orbit env sync                         -> orbit source sync
  orbit env sync --dry                   -> orbit source sync --dry
  orbit env sync --url URL               -> orbit source update NAME --url URL
  orbit env sync --ref REF               -> orbit source update NAME --ref REF
  orbit env sync --path PATH             -> orbit source update NAME --path PATH

Run "orbit source list" to find NAME. Use "orbit source add" when the source
does not exist.`,
		RunE: runEnvSync,
	}
	cmd.Flags().StringVar(&envSyncURL, "url", "", "deprecated; use orbit source update NAME --url URL")
	cmd.Flags().StringVar(&envSyncRef, "ref", "", "deprecated; use orbit source update NAME --ref REF")
	cmd.Flags().StringVar(&envSyncPath, "path", "", "deprecated; use orbit source update NAME --path PATH")
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
		return legacyEnvSyncSourceChange(urlChanged, pathChanged, refChanged)
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

func legacyEnvSyncSourceChange(urlChanged, pathChanged, refChanged bool) error {
	message := "legacy repository flags cannot be translated without choosing a persistent source; no changes were made"
	action := cli.JSONAction{
		Command: "orbit source list --json",
		Reason:  "List sources and identify the source to update.",
	}
	if urlChanged && pathChanged {
		return cli.WithJSONReplacementActions(cli.NewInvalidArgumentError("--url and --path are mutually exclusive; no changes were made"), []cli.JSONAction{action})
	}
	if pathChanged && refChanged {
		return cli.WithJSONReplacementActions(cli.NewInvalidArgumentError("--ref is valid only with a Git source; no changes were made"), []cli.JSONAction{action})
	}
	if urlChanged && strings.TrimSpace(envSyncURL) == "" || pathChanged && strings.TrimSpace(envSyncPath) == "" || refChanged && strings.TrimSpace(envSyncRef) == "" {
		return cli.WithJSONReplacementActions(cli.NewInvalidArgumentError("legacy repository flags cannot be empty; no changes were made"), []cli.JSONAction{action})
	}
	if envSyncDryRun || envSyncYes || envSyncNoApply {
		return cli.WithJSONReplacementActions(cli.NewInvalidArgumentError(message+"; run the source command explicitly so every flag remains intentional"), []cli.JSONAction{action})
	}
	registry, err := sourceRegistry()
	if err != nil {
		return err
	}
	defaultSource, defaultErr := registry.Default()
	switch {
	case errors.Is(defaultErr, envsource.ErrNotFound) && urlChanged:
		command := "orbit source add default --url " + shellquote.Quote(envSyncURL)
		if envSyncRef != "" {
			command += " --ref " + shellquote.Quote(envSyncRef)
		}
		action = cli.JSONAction{Command: command + " --json", Reason: "Add the legacy Git repository as the first source."}
		message = "legacy repository flags no longer configure env sync; add the source explicitly with: " + command
	case errors.Is(defaultErr, envsource.ErrNotFound):
		message = "legacy repository flags no longer configure env sync; run 'orbit source list', then add a named source"
	case defaultErr != nil:
		return defaultErr
	case urlChanged && defaultSource.Type == envsource.TypeGit:
		command := "orbit source update " + shellquote.Quote(defaultSource.Name) + " --url " + shellquote.Quote(envSyncURL)
		if envSyncRef != "" {
			command += " --ref " + shellquote.Quote(envSyncRef)
		}
		action = cli.JSONAction{Command: command + " --json", Reason: "Update and synchronize the default Git source."}
		message = "legacy repository flags no longer configure env sync; update the default source with: " + command
	case refChanged && defaultSource.Type == envsource.TypeGit:
		command := "orbit source update " + shellquote.Quote(defaultSource.Name) + " --ref " + shellquote.Quote(envSyncRef)
		action = cli.JSONAction{Command: command + " --json", Reason: "Update and synchronize the default Git source."}
		message = "legacy repository flags no longer configure env sync; update the default source with: " + command
	case pathChanged && defaultSource.Type == envsource.TypeLocal:
		command := "orbit source update " + shellquote.Quote(defaultSource.Name) + " --path " + shellquote.Quote(envSyncPath)
		action = cli.JSONAction{Command: command + " --json", Reason: "Update and synchronize the default local source."}
		message = "legacy repository flags no longer configure env sync; update the default source with: " + command
	default:
		message = "legacy repository flags would change the default source type; run 'orbit source list' and add a new named source"
	}
	return cli.WithJSONReplacementActions(cli.NewInvalidArgumentError(message), []cli.JSONAction{action})
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
