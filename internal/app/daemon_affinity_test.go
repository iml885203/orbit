package app

import (
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/instance"

	"github.com/iml885203/orbit/internal/bigquery"
	"github.com/iml885203/orbit/internal/devdb"
	"github.com/iml885203/orbit/internal/extension"
	"github.com/iml885203/orbit/internal/tunnel"
	"github.com/spf13/cobra"
)

func daemonAffinityRoot(t *testing.T) *cobra.Command {
	t.Helper()
	oldExtensions := extensions
	extensions = []extension.Extension{{Commands: func() []*cobra.Command {
		return []*cobra.Command{devdb.SQLServerCmd(), bigquery.Command(), tunnel.TunnelCmd()}
	}}}
	t.Cleanup(func() { extensions = oldExtensions })
	return newRootCommand()
}

func TestDaemonAffinityConstructedCommandTree(t *testing.T) {
	root := daemonAffinityRoot(t)
	matching := map[string]bool{}
	for _, name := range []string{"down", "restart", "logs", "open", "exec", "query", "topics", "seed", "edge", "service", "sqlserver", "tunnel", "bq", "trace", "tracing"} {
		matching[name] = true
	}
	reconciled := map[string]bool{}
	for _, name := range []string{"restart", "exec", "query", "topics", "seed", "edge", "service", "sqlserver", "tunnel"} {
		reconciled[name] = true
	}
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		top := cmd
		for top.Parent() != root && top.Parent() != nil {
			top = top.Parent()
		}
		wantMatch := matching[top.Name()]
		wantReconcile := reconciled[top.Name()] || (top.Name() == "env" && cmd.Name() == "toggle") || (top.Name() == "daemon" && cmd.Name() == "start")
		if got := commandRequiresMatchingDaemonConfig(cmd); got != wantMatch {
			t.Errorf("%s config matching = %v, want %v", cmd.CommandPath(), got, wantMatch)
		}
		if got := commandRequiresReconciledDaemon(cmd); got != wantReconcile {
			t.Errorf("%s reconciliation = %v, want %v", cmd.CommandPath(), got, wantReconcile)
		}
		for _, alias := range cmd.Aliases {
			args := []string{alias}
			for parent := cmd.Parent(); parent != root; parent = parent.Parent() {
				args = append([]string{parent.Name()}, args...)
			}
			resolved, _, err := root.Find(args)
			if err != nil || resolved != cmd {
				t.Errorf("alias %v did not resolve to %s: %v", args, cmd.CommandPath(), err)
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	for _, cmd := range root.Commands() {
		visit(cmd)
	}
}

func TestExtensionDaemonAffinityGuardsCommandExecution(t *testing.T) {
	for _, test := range []struct {
		name                     string
		affinity                 cli.DaemonAffinity
		mismatch, stale, blocked bool
	}{
		{"matching config mismatch", cli.DaemonMatchingConfig, true, false, true},
		{"matching stale config", cli.DaemonMatchingConfig, false, true, false},
		{"matching reconciled mismatch", cli.DaemonMatchingReconciledConfig, true, false, true},
		{"matching reconciled stale", cli.DaemonMatchingReconciledConfig, false, true, true},
		{"reconcile different config", cli.DaemonReconciledConfig, true, false, false},
		{"reconcile stale config", cli.DaemonReconciledConfig, false, true, true},
		{"independent mismatched stale config", cli.DaemonIndependent, true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			status := readyFixture(t)
			selected := configFile
			if test.mismatch {
				status.ConfigPath = filepath.Join(filepath.Dir(selected), "other.yaml")
			}
			status.ConfigStale = test.stale
			var executed atomic.Bool
			oldExtensions := extensions
			extensions = []extension.Extension{{Name: "renamed-feature", Commands: func() []*cobra.Command {
				parent := &cobra.Command{Use: "new-feature"}
				parent.AddCommand(&cobra.Command{Use: "nested", Aliases: []string{"alias"}, RunE: func(*cobra.Command, []string) error {
					executed.Store(true)
					return nil
				}})
				return []*cobra.Command{cli.WithDaemonAffinity(parent, test.affinity)}
			}}}
			t.Cleanup(func() { extensions = oldExtensions })
			serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/health":
					w.WriteHeader(http.StatusOK)
				case "/api/status":
					daemon.WriteJSON(w, http.StatusOK, status)
				case "/api/version":
					daemon.WriteJSON(w, http.StatusOK, daemon.VersionResponse{Running: buildVersion()})
				default:
					http.NotFound(w, r)
				}
			})
			_, _, err := executeAppCommand(t, "--config", selected, "--json", "new-feature", "alias")
			if (err != nil) != test.blocked || executed.Load() == test.blocked {
				t.Fatalf("blocked=%v executed=%v err=%v", test.blocked, executed.Load(), err)
			}
			if test.blocked {
				var mismatch *daemon.ConfigMismatchError
				var stale *daemon.ConfigStaleError
				if test.mismatch && !errors.As(err, &mismatch) {
					t.Fatalf("error=%T %v, want config mismatch", err, err)
				}
				if !test.mismatch && !errors.As(err, &stale) {
					t.Fatalf("error=%T %v, want stale config", err, err)
				}
			}
		})
	}
}

func TestExtensionCommandMustDeclareDaemonAffinity(t *testing.T) {
	oldExtensions := extensions
	extensions = []extension.Extension{{Name: "new-feature", Commands: func() []*cobra.Command {
		return []*cobra.Command{{Use: "missing-declaration"}}
	}}}
	t.Cleanup(func() { extensions = oldExtensions })
	defer func() {
		if got := recover(); got == nil {
			t.Fatal("extension command without explicit daemon affinity was accepted")
		}
	}()
	newRootCommand()
}

func TestDaemonAffinityCoreGuardsAndRecoveryExecution(t *testing.T) {
	for _, test := range []struct {
		name                     string
		args                     []string
		mismatch, stale, blocked bool
	}{
		{"core matching", []string{"logs", "api"}, true, false, true},
		{"core reconciled", []string{"service", "mode", "api", "dev"}, false, true, true},
		{"toggle reconciled", []string{"env", "toggle", "api", "DEBUG", "on"}, false, true, true},
		{"daemon start reconciled", []string{"daemon", "start"}, false, true, true},
		{"status recovery", []string{"status"}, false, true, false},
		{"environment info recovery", []string{"env", "info"}, true, true, false},
		{"completion recovery", []string{"completion", "bash"}, true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			status := readyFixture(t)
			t.Setenv(instance.EnvName, "")
			selected := configFile
			if test.mismatch {
				status.ConfigPath = filepath.Join(filepath.Dir(selected), "other.yaml")
			}
			status.ConfigStale = test.stale
			var mutations atomic.Int32
			serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet && r.URL.Path != "/api/history/cli-event" {
					mutations.Add(1)
					http.NotFound(w, r)
					return
				}
				switch r.URL.Path {
				case "/api/health":
					w.WriteHeader(http.StatusOK)
				case "/api/status":
					daemon.WriteJSON(w, http.StatusOK, status)
				case "/api/version":
					daemon.WriteJSON(w, http.StatusOK, daemon.VersionResponse{Running: buildVersion()})
				default:
					http.NotFound(w, r)
				}
			})
			args := append([]string{"--config", selected, "--json"}, test.args...)
			_, _, err := executeAppCommand(t, args...)
			if (err != nil) != test.blocked {
				t.Fatalf("blocked=%v err=%v", test.blocked, err)
			}
			if mutations.Load() != 0 {
				t.Fatalf("guard/recovery command sent %d mutations", mutations.Load())
			}
		})
	}
}
