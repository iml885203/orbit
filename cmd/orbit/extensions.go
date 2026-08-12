// The official distribution composes Orbit's generic database and tunnel
// features. Team-specific environments and workspace conventions live in
// environment repositories rather than in the binary.
package main

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/orbit/internal/devdb"
	"github.com/iml885203/orbit/internal/tunnel"
)

// Extensions returns the feature set shipped in the official binary.
func Extensions() []extension.Extension {
	return []extension.Extension{{
		Name: "official",
		Commands: func() []*cobra.Command {
			return []*cobra.Command{
				devdb.SQLServerCmd(),
				tunnel.TunnelCmd(),
			}
		},
		CommandVisibility: func(cfg *config.Config) map[string]bool {
			return map[string]bool{
				"sqlserver": cfg != nil && devdb.SQLServerFrom(cfg) != nil,
				"tunnel":    cfg != nil && tunnel.ClaimFrom(cfg) != nil,
			}
		},
		DaemonSetup: officialDaemonSetup,
		CLIDoctor: &extension.CLIDoctor{
			Checks:     devdb.CLIDoctorChecks,
			PrintHuman: devdb.PrintDBWorkflowChecks,
		},
		Distribution: &extension.Distribution{
			EnvRepoURL: "https://github.com/iml885203/orbit-demo.git",
			EnvRepoRef: "v0.13.0",
			InstallURL: "https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh",
			DefaultEnv: "quickstart.yaml",
		},
	}}
}

// officialDaemonSetup merges the hooks for the independently gated database
// and tunnel features before the daemon starts serving.
func officialDaemonSetup(host extension.Host, mux *http.ServeMux) extension.DaemonHooks {
	h := devdb.SetupDaemon(host, mux)
	h.Merge(tunnel.SetupDaemon(host, mux))
	return h
}
