package tunnel

import (
	"fmt"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/tunlease/pkg/tunnelcli"
	"github.com/spf13/cobra"
)

// TunnelCmd groups the local-dev 3rd-party-callback tunnel commands under
// `orbit tunnel ...`, matching the noun-subcommand pattern of service/db/topics.
func TunnelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tunnel",
		Short: "Route staging 3rd-party callbacks to your local machine",
		Long: "Open a Tunlease tunnel and claim 3rd-party callback " +
			"paths so they reach a service on your laptop. Not tied to `orbit up`.",
	}
	cmd.AddCommand(tunnelClaimCmd(), tunnelReleaseCmd(), tunnelListCmd())
	return cmd
}

func tunnelClaimCmd() *cobra.Command {
	var flags tunnelcli.ClaimFlags
	cmd := &cobra.Command{
		Use:     tunnelcli.ClaimUse,
		Short:   tunnelcli.ClaimShort,
		Example: tunnelcli.ClaimExample("orbit tunnel claim"),
		Args:    tunnelArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) (runErr error) {
			defer func() { runErr = renderTunnelCommandError(cmd, runErr) }()
			options, err := flags.Options(args)
			if err != nil {
				return err
			}
			if cli.JSONOutput {
				options.Output = "json"
			}
			client, err := daemon.Dial(daemon.DefaultSocketPath())
			if err != nil {
				return err
			}
			if _, err := claimPaths(client, options); err != nil {
				return fmt.Errorf("claim failed: %w", err)
			}
			out := newTunnelOutput(cmd.OutOrStdout(), options.Output)
			out.connected(options.Paths, options.To, options.Detach)
			if options.Detach {
				return nil
			}
			return waitForClaim(cmd.Context(), client, options, out)
		},
	}
	tunnelcli.BindClaimFlagsWithReleaseCommand(cmd, &flags, "orbit tunnel release")
	bindTunnelFlagErrors(cmd)
	return cmd
}

func tunnelReleaseCmd() *cobra.Command {
	var flags tunnelcli.ReleaseFlags
	cmd := &cobra.Command{
		Use:   tunnelcli.ReleaseUse,
		Short: tunnelcli.ReleaseShort,
		Args:  tunnelArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) (runErr error) {
			defer func() { runErr = renderTunnelCommandError(cmd, runErr) }()
			resolved, err := flags.Resolve()
			if err != nil {
				return err
			}
			if cli.JSONOutput {
				resolved.Output = "json"
			}
			client, err := daemon.Dial(daemon.DefaultSocketPath())
			if err != nil {
				return err
			}
			out := newTunnelOutput(cmd.OutOrStdout(), resolved.Output)
			switch {
			case resolved.To != 0 && len(args) != 0:
				return fmt.Errorf("specify either PATH or --to, not both")
			case resolved.To != 0:
				result, err := releaseTunnel(client, resolved.To, resolved)
				if err != nil {
					return fmt.Errorf("release failed: %w", err)
				}
				out.releaseSummary(resolved.To, result.Released, result.Gateway)
			case len(args) == 1:
				if _, err := releaseWithOptions(client, args[0], resolved); err != nil {
					return fmt.Errorf("release failed: %w", err)
				}
				out.released([]string{args[0]}, 0)
			default:
				return fmt.Errorf("specify a PATH or --to PORT")
			}
			return nil
		},
	}
	tunnelcli.BindReleaseFlags(cmd, &flags)
	bindTunnelFlagErrors(cmd)
	return cmd
}

func tunnelListCmd() *cobra.Command {
	var flags tunnelcli.ListFlags
	cmd := &cobra.Command{
		Use:   tunnelcli.ListUse,
		Short: tunnelcli.ListShort,
		Args:  tunnelArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) (runErr error) {
			defer func() { runErr = renderTunnelCommandError(cmd, runErr) }()
			resolved, err := flags.Resolve()
			if err != nil {
				return err
			}
			if cli.JSONOutput {
				resolved.Output = "json"
			}
			client, err := daemon.Dial(daemon.DefaultSocketPath())
			if err != nil {
				return err
			}
			out := newTunnelOutput(cmd.OutOrStdout(), resolved.Output)
			if resolved.All {
				claims, err := globalClaims(client, resolved)
				if err != nil {
					return err
				}
				out.globalClaims(claims)
				return nil
			}
			state, err := listTunnelState(client)
			if err != nil {
				return err
			}
			out.tunnels(state)
			return nil
		},
	}
	tunnelcli.BindListFlags(cmd, &flags)
	bindTunnelFlagErrors(cmd)
	return cmd
}
