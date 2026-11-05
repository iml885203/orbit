package app

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandStringPreservesFlagsAndQuotesArgs(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"./bin/orbit", "up", "--infra", "--timeout", "60s", "api service"}
	if got, want := commandString(), "orbit up --infra --timeout 60s 'api service'"; got != want {
		t.Fatalf("commandString() = %q, want %q", got, want)
	}
}

func TestCommandStringQuotesSingleQuotes(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"orbit", "sqlserver", "query", "SELECT * FROM Users WHERE Name='Logan'"}
	if got, want := commandString(), "orbit sqlserver query 'SELECT * FROM Users WHERE Name='\"'\"'Logan'\"'\"''"; got != want {
		t.Fatalf("commandString() = %q, want %q", got, want)
	}
}

func TestCommandStringRedactsURLCredentials(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"orbit", "source", "add", "x",
		"--url", "https://user:secret@host/repo?access_token=secret",
		"--url=https://secret@host/other",
		"https://host/positional?token=secret",
		"--json"}
	got := commandString()
	if want := "orbit source add x --url https://host/repo --url=https://host/other https://host/positional --json"; got != want {
		t.Fatalf("commandString() = %q, want %q", got, want)
	}
}

func TestCommandStringLeavesNonURLArgumentsAsTyped(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	query := "SELECT 'https://a@b/c?d' AS u"
	os.Args = []string{"orbit", "sqlserver", "query", query, "--filter=a?b"}
	if got, want := commandString(), "orbit sqlserver query 'SELECT '\"'\"'https://a@b/c?d'\"'\"' AS u' '--filter=a?b'"; got != want {
		t.Fatalf("commandString() = %q, want %q", got, want)
	}
}

func TestShouldRecordCLIDaemonLifecycleCommands(t *testing.T) {
	root := &cobra.Command{Use: "orbit"}
	daemonCmd := &cobra.Command{Use: "daemon"}
	restartCmd := &cobra.Command{Use: "restart", Run: func(*cobra.Command, []string) {}}
	statusCmd := &cobra.Command{Use: "status", Run: func(*cobra.Command, []string) {}}
	daemonCmd.AddCommand(restartCmd, statusCmd)
	root.AddCommand(daemonCmd)

	if !shouldRecordCLI(restartCmd) {
		t.Fatal("daemon restart should be recorded as a mutating user command")
	}
	if shouldRecordCLI(statusCmd) {
		t.Fatal("daemon status should not be recorded")
	}
}

func TestShouldNotRecordUninstall(t *testing.T) {
	if shouldRecordCLI(uninstallCmd()) {
		t.Fatal("uninstall must not recreate user data through command history")
	}
}

func TestShouldNotRecordSkillInstall(t *testing.T) {
	for _, cmd := range skillCmd().Commands() {
		if shouldRecordCLI(cmd) {
			t.Fatalf("skill %s changes agent setup, not the environment; history must not replay it", cmd.Name())
		}
	}
}
