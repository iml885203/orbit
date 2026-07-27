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

	os.Args = []string{"orbit", "db", "query", "SELECT * FROM Users WHERE Name='Logan'"}
	if got, want := commandString(), "orbit db query 'SELECT * FROM Users WHERE Name='\"'\"'Logan'\"'\"''"; got != want {
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
