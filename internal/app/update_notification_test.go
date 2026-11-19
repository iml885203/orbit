package app

import (
	"testing"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/extension"
	"github.com/spf13/cobra"
)

func TestUpdateNotificationOnlyFollowsFiniteInteractiveProductCommands(t *testing.T) {
	oldDistribution, oldJSON := distribution, cli.JSONOutput
	t.Cleanup(func() { distribution, cli.JSONOutput = oldDistribution, oldJSON })
	distribution = extension.Distribution{ReleaseAPIURL: "https://example.invalid/releases/latest"}
	cli.JSONOutput = false
	for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "TF_BUILD", "JENKINS_URL", "BUILD_BUILDID", backgroundUpdateEnv} {
		t.Setenv(key, "")
	}
	root := newRootCommand()
	for _, name := range []string{"up", "down", "restart", "status", "inspect", "doctor", "logs", "trace", "tunnel", "exec", "update", "version", "help", "completion", "__update-check", "__update-apply"} {
		t.Run(name, func(t *testing.T) {
			cmd, _, err := root.Find([]string{name})
			if err != nil || cmd == root {
				cmd = &cobra.Command{Use: name}
				root.AddCommand(cmd)
			}
			want := name == "up" || name == "down" || name == "restart" || name == "status"
			if got := updateNotificationEligible(cmd, true); got != want {
				t.Fatalf("eligible = %v, want %v", got, want)
			}
			if updateNotificationEligible(cmd, false) {
				t.Fatal("notification in redirected execution")
			}
		})
	}
	status, _, _ := root.Find([]string{"status"})
	cli.JSONOutput = true
	if updateNotificationEligible(status, true) {
		t.Fatal("notification in JSON output")
	}
	cli.JSONOutput = false
	t.Setenv("CI", "true")
	if updateNotificationEligible(status, true) {
		t.Fatal("notification in CI")
	}
	t.Setenv("CI", "")
	t.Setenv(backgroundUpdateEnv, "1")
	if updateNotificationEligible(status, true) {
		t.Fatal("notification in background worker")
	}
	t.Setenv(backgroundUpdateEnv, "")
	suppressUpdateNotification(status)
	if updateNotificationEligible(status, true) {
		t.Fatal("duplicate notification after existing recovery guidance")
	}
}
