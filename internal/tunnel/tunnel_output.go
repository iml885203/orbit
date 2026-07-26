package tunnel

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/spf13/cobra"
)

type tunnelOutput struct {
	writer io.Writer
	json   bool
}

func renderTunnelCommandError(cmd *cobra.Command, err error) error {
	if err == nil || cli.JSONOutput {
		return err
	}
	output, flagErr := cmd.Flags().GetString("output")
	if flagErr != nil || output != "json" {
		return err
	}
	_ = json.NewEncoder(cmd.ErrOrStderr()).Encode(map[string]any{
		"schema_version": 1,
		"type":           "error",
		"code":           "command_failed",
		"message":        err.Error(),
	})
	return cli.MarkJSONErrorRendered(err)
}

func tunnelArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		return renderTunnelCommandError(cmd, validate(cmd, args))
	}
}

func bindTunnelFlagErrors(cmd *cobra.Command) {
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return renderTunnelCommandError(cmd, err)
	})
}

func newTunnelOutput(writer io.Writer, format string) *tunnelOutput {
	return &tunnelOutput{writer: writer, json: format == "json"}
}

func (out *tunnelOutput) event(fields map[string]any) {
	fields["schema_version"] = 1
	_ = json.NewEncoder(out.writer).Encode(fields)
}

func (out *tunnelOutput) connected(paths []string, port int, background bool) {
	if out.json {
		event := map[string]any{
			"type": "connected", "paths": paths, "target": fmt.Sprintf("localhost:%d", port),
			"local_port": port,
		}
		if background {
			event["background"] = true
			event["release_command"] = fmt.Sprintf("orbit tunnel release --to %d", port)
		}
		out.event(event)
		return
	}
	state := "Connected"
	if background {
		state = "Connected in background"
	}
	fmt.Fprintf(out.writer, "%s: %s → localhost:%d\n", state, strings.Join(paths, " "), port)
	if background {
		fmt.Fprintf(out.writer, "Stop with: orbit tunnel release --to %d\n", port)
		return
	}
	fmt.Fprintln(out.writer, "Waiting for requests… (Ctrl+C to release)")
}

func (out *tunnelOutput) request(line AccessLine) {
	if out.json {
		out.event(map[string]any{
			"type": "request", "method": line.Method, "path": line.Path,
			"status": line.Status, "duration_ms": line.DurationMs,
		})
		return
	}
	duration := time.Duration(line.DurationMs) * time.Millisecond
	if duration < time.Millisecond {
		fmt.Fprintf(out.writer, "→ %s %s  %d  <1ms\n", line.Method, line.Path, line.Status)
		return
	}
	fmt.Fprintf(out.writer, "→ %s %s  %d  %s\n", line.Method, line.Path, line.Status, duration)
}

func (out *tunnelOutput) released(paths []string, port int) {
	if out.json {
		event := map[string]any{"type": "released", "paths": paths}
		if port != 0 {
			event["local_port"] = port
		}
		out.event(event)
		return
	}
	fmt.Fprintf(out.writer, "Released: %s\n", strings.Join(paths, " "))
}

func (out *tunnelOutput) releaseSummary(port, released int, gateway string) {
	if out.json {
		out.event(map[string]any{
			"type": "release_summary", "released": released, "failed": 0,
			"local_port": port, "gateway": gateway,
		})
		return
	}
	if released == 0 {
		fmt.Fprintf(out.writer, "No claims for localhost:%d.\n", port)
		return
	}
	fmt.Fprintf(out.writer, "Released %d claim(s) for localhost:%d.\n", released, port)
}

func (out *tunnelOutput) tunnels(state TunnelListResponse) {
	if out.json {
		claims := make([]map[string]any, 0, len(state.Claims))
		for _, claim := range state.Claims {
			item := map[string]any{
				"paths": claim.Paths, "owner": claim.Owner, "started_at": claim.StartedAt,
				"target":     fmt.Sprintf("localhost:%d", claim.LocalPort),
				"local_port": claim.LocalPort, "mine": true, "status": claim.Status,
			}
			if claim.ExpiresAt != "" {
				item["expires_at"] = claim.ExpiresAt
			}
			claims = append(claims, item)
		}
		out.event(map[string]any{"type": "claim_list", "claims": claims})
		return
	}
	if len(state.Claims) == 0 {
		fmt.Fprintln(out.writer, "No active claims. Use --all to include others.")
		return
	}
	for _, claim := range state.Claims {
		fmt.Fprintf(out.writer, "%s → localhost:%d  %s\n", strings.Join(claim.Paths, ","), claim.LocalPort, claim.Status)
	}
}

func (out *tunnelOutput) globalClaims(claims []GlobalClaimView) {
	if out.json {
		items := make([]map[string]any, 0, len(claims))
		for _, claim := range claims {
			item := map[string]any{
				"paths": []string{claim.PathPrefix}, "owner": claim.Owner,
				"started_at": claim.StartedAt, "mine": claim.Mine, "status": "connected",
			}
			if claim.ExpiresAt != "" {
				item["expires_at"] = claim.ExpiresAt
			}
			items = append(items, item)
		}
		out.event(map[string]any{"type": "claim_list", "claims": items})
		return
	}
	if len(claims) == 0 {
		fmt.Fprintln(out.writer, "No active claims.")
		return
	}
	for _, claim := range claims {
		suffix := ""
		if claim.Mine {
			suffix = "  (you)"
		}
		fmt.Fprintf(out.writer, "%s owner=%s%s\n", claim.PathPrefix, claim.Owner, suffix)
	}
}
