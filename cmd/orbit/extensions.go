// The official distribution composes Orbit's SQL Server, BigQuery and tunnel
// features. Team-specific environments and workspace conventions live in
// environment repositories rather than in the binary.
package main

import (
	"github.com/iml885203/orbit/internal/bigquery"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/devdb"
	"github.com/iml885203/orbit/internal/distribution"
	"github.com/iml885203/orbit/internal/extension"
	"github.com/iml885203/orbit/internal/tunnel"
)

// Extensions returns the feature set shipped in the official binary.
func Extensions() []extension.Extension {
	official := distribution.Official()
	return []extension.Extension{{
		Name: "official",
		Commands: func() []*cobra.Command {
			return []*cobra.Command{
				devdb.SQLServerCmd(),
				bigquery.Command(),
				tunnel.TunnelCmd(),
			}
		},
		CommandVisibility: func(cfg *config.Config) map[string]bool {
			return map[string]bool{
				"bq":        bigquery.From(cfg) != nil,
				"sqlserver": cfg != nil && devdb.SQLServerFrom(cfg) != nil,
				"tunnel":    cfg != nil && tunnel.ClaimFrom(cfg) != nil,
			}
		},
		DaemonSetup: officialDaemonSetup,
		CLIDoctor: &extension.CLIDoctor{
			Checks: func(cfg *config.Config, path string) []daemon.DoctorCheck {
				return append(devdb.CLIDoctorChecks(cfg, path), bigquery.DoctorChecks(cfg, path)...)
			},
			PrintHuman: func(cfg *config.Config) { devdb.PrintDBWorkflowChecks(cfg); bigquery.PrintDoctorChecks(cfg) },
		},
		Distribution: &official,
	}}
}

// officialDaemonSetup merges the hooks for the independently gated database
// and tunnel features before the daemon starts serving.
func officialDaemonSetup(host extension.Host, mux *http.ServeMux) extension.DaemonHooks {
	h := devdb.SetupDaemon(host, mux)
	h.Merge(tunnel.SetupDaemon(host, mux))
	h.Merge(bigquery.SetupDaemon(host, mux))
	return h
}
