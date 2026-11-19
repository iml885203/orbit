package app

import (
	"os"
	"strings"
	"time"

	"github.com/iml885203/orbit/internal/autoupdate"
	"github.com/iml885203/orbit/internal/cli"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

const updateNoticeSuppressed = "orbit.update-notice-suppressed"

func showUpdateNotification(cmd *cobra.Command) {
	interactive := (isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())) &&
		(isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd()))
	if !updateNotificationEligible(cmd, interactive) {
		return
	}
	launch, err := invokedBinaryPath()
	if err == nil {
		_ = autoupdate.Notify(launch, buildVersion(), time.Now(), os.Stderr)
	}
}

func updateNotificationEligible(cmd *cobra.Command, interactive bool) bool {
	if cmd == nil || !interactive || cli.JSONOutput || distribution.ReleaseAPIURL == "" || os.Getenv(backgroundUpdateEnv) == "1" {
		return false
	}
	for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "TF_BUILD", "JENKINS_URL", "BUILD_BUILDID"} {
		value := strings.ToLower(os.Getenv(key))
		if value != "" && value != "false" && value != "0" {
			return false
		}
	}
	if cmd.Annotations[updateNoticeSuppressed] == "true" || cmd.Parent() != cmd.Root() {
		return false
	}
	if help, _ := cmd.Flags().GetBool("help"); help {
		return false
	}
	switch cmd.Name() {
	case "up", "down", "restart", "status":
		return true
	default:
		return false
	}
}

func suppressUpdateNotification(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	cmd.Annotations[updateNoticeSuppressed] = "true"
}
