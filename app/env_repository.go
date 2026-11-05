package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/iml885203/orbit/cli"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsync"
)

const (
	settingKeyEnvRepoURL = "env_repo_url"
	settingKeyEnvRepoRef = "env_repo_ref"
)

func envRepoSyncError(err error) error {
	return envRepoAccessError(err, "orbit source sync", cli.WithJSONActions)
}

// envRepoAccessError explains why an environment repository could not be read
// and names retry as the step to run once access is restored. attach decides
// whether its actions add to or replace the classified env_repo_access default.
func envRepoAccessError(err error, retry string, attach func(error, []cli.JSONAction) error) error {
	var cloneErr *envsync.CloneError
	if !errors.As(err, &cloneErr) {
		return fmt.Errorf("sync: %w", err)
	}
	retryAction := cli.JSONAction{
		Command:     retry + " --json",
		Reason:      "Retry after restoring Git access to the environment repository.",
		Destructive: false,
	}
	if errors.Is(cloneErr.Err, exec.ErrNotFound) {
		return attach(
			cli.NewEnvRepoAccessError(
				"Git is required only to sync shared environments, but git was not found on PATH.\n"+
					"Install Git from https://git-scm.com/downloads, then retry `"+retry+"`.",
			),
			[]cli.JSONAction{retryAction},
		)
	}
	message := fmt.Sprintf("cannot access environment repo %s: %v", cloneErr.DisplayURL(), cloneErr.Err)
	if detail := strings.TrimSpace(cloneErr.RedactedOutput()); detail != "" {
		message += "\nGit: " + detail
	}
	if cloneErr.ReportsAmbiguousGitHubAvailability() {
		message += "\nCheck the GitHub owner and repository name first. GitHub uses the same response when a private repository is hidden from your current credentials."
		message += "\nIf the URL is correct and the repo is private, authenticate Git; otherwise correct the repository URL."
		return cli.NewEnvRepoUnavailableError(message)
	}
	if cloneErr.IsGitHub() {
		message += "\nFor a private GitHub repo, run 'gh auth login' and 'gh auth setup-git', then retry `" + retry + "`."
		return attach(cli.NewEnvRepoAccessError(message), []cli.JSONAction{
			retryAction,
			{Command: "gh auth login", Reason: "Authenticate GitHub CLI for private repository access.", Destructive: false},
			{Command: "gh auth setup-git", Reason: "Configure Git to use the authenticated GitHub CLI credentials.", Destructive: false},
		})
	}
	message += "\nVerify the repository URL and Git access, then retry `" + retry + "`."
	return attach(cli.NewEnvRepoAccessError(message), []cli.JSONAction{retryAction})
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
