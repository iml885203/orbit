package tunnel

// Tunnel client calls moved from the daemon client when the tunnel
// feature became extension-owned (spec B6): they build on the exported
// Client primitives (PostJSON / GetDecode) so the core client stays
// feature-free.

import (
	"errors"
	"fmt"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/tunlease/pkg/tunnelcli"
)

// Claim registers one or more path claims sharing the tunnel for localPort.
func claimPaths(c *daemon.Client, options tunnelcli.ClaimOptions) (*daemon.APIResponse, error) {
	return c.PostJSON("/api/tunnel/claim", ClaimAPIRequest{
		Gateway: options.Gateway, Token: options.Token, Insecure: options.Insecure,
		LocalPort: options.To, Paths: options.Paths,
	})
}

// ReleasePath releases a single claimed path.
func releasePath(c *daemon.Client, path string) (*ReleaseAPIResponse, error) {
	var out ReleaseAPIResponse
	if err := c.PostDecode("/api/tunnel/release", ReleaseAPIRequest{Path: path}, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, errors.New(out.Error)
	}
	return &out, nil
}

// ReleaseTunnel releases every claim on the tunnel for localPort.
func releaseTunnel(c *daemon.Client, localPort int, flags tunnelcli.ReleaseFlags) (*ReleaseAPIResponse, error) {
	var out ReleaseAPIResponse
	if err := c.PostDecode("/api/tunnel/release", ReleaseAPIRequest{
		LocalPort: localPort, Gateway: flags.Gateway, Token: flags.Token, Insecure: flags.Insecure,
	}, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, errors.New(out.Error)
	}
	return &out, nil
}

func releaseWithOptions(c *daemon.Client, path string, flags tunnelcli.ReleaseFlags) (*ReleaseAPIResponse, error) {
	request := ReleaseAPIRequest{
		Gateway: flags.Gateway, Token: flags.Token, Insecure: flags.Insecure, Path: path,
	}
	var out ReleaseAPIResponse
	if err := c.PostDecode("/api/tunnel/release", request, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, errors.New(out.Error)
	}
	return &out, nil
}

// Tunnels lists current tunnels and their claims.
func listTunnels(c *daemon.Client) ([]TunnelView, error) {
	out, err := listTunnelState(c)
	if err != nil {
		return nil, err
	}
	return out.Tunnels, nil
}

func listTunnelState(c *daemon.Client) (TunnelListResponse, error) {
	var out TunnelListResponse
	if err := c.GetDecode("/api/tunnel", &out); err != nil {
		return TunnelListResponse{}, fmt.Errorf("tunnel list request failed: %w", err)
	}
	return out, nil
}

// GlobalClaims lists every claim on the configured Tunlease gateway.
func globalClaims(c *daemon.Client, flags tunnelcli.ListFlags) ([]GlobalClaimView, error) {
	var out GlobalClaimsResponse
	if err := c.PostDecode("/api/tunnel/list", ClientAPIOptions{
		Gateway: flags.Gateway, Token: flags.Token, Insecure: flags.Insecure,
	}, &out); err != nil {
		return nil, fmt.Errorf("global claims request failed: %w", err)
	}
	return out.Claims, nil
}
