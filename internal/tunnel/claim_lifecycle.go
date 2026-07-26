package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/tunlease/pkg/tunnelcli"
)

func waitForClaim(parent context.Context, client *daemon.Client, options tunnelcli.ClaimOptions, out *tunnelOutput) error {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	activity := make(chan AccessLine, 16)
	streamErr := make(chan error, 1)
	startedAt := time.Now()
	go func() {
		streamErr <- streamTunnelAccess(ctx, client, options.To, startedAt, activity)
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case line := <-activity:
			out.request(line)
		case <-ctx.Done():
			if err := releaseClaimPaths(client, options.Paths); err != nil {
				return err
			}
			out.released(options.Paths, options.To)
			return nil
		case err := <-streamErr:
			if ctx.Err() != nil {
				continue
			}
			return err
		case <-ticker.C:
			active, err := claimedPaths(client, options.To)
			if err != nil {
				return err
			}
			if !containsEvery(active, options.Paths) {
				out.released(options.Paths, options.To)
				return nil
			}
		}
	}
}

func releaseClaimPaths(client *daemon.Client, paths []string) error {
	for _, path := range paths {
		if _, err := releasePath(client, path); err != nil {
			return fmt.Errorf("release %s: %w", path, err)
		}
	}
	return nil
}

func claimedPaths(client *daemon.Client, port int) ([]string, error) {
	tunnels, err := listTunnels(client)
	if err != nil {
		return nil, err
	}
	for _, tunnel := range tunnels {
		if tunnel.LocalPort == port {
			if tunnel.Status == statusError {
				return nil, fmt.Errorf("tunnel failed: %s", tunnel.LastError)
			}
			return tunnel.Paths, nil
		}
	}
	return nil, nil
}

func containsEvery(have, want []string) bool {
	set := make(map[string]bool, len(have))
	for _, path := range have {
		set[path] = true
	}
	for _, path := range want {
		if !set[path] {
			return false
		}
	}
	return true
}

func streamTunnelAccess(ctx context.Context, client *daemon.Client, port int, startedAt time.Time, activity chan<- AccessLine) error {
	resp, err := client.Get("/api/events")
	if err != nil {
		return fmt.Errorf("events stream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	go func() {
		<-ctx.Done()
		_ = resp.Body.Close()
	}()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var event string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case event == "tunnel-access" && strings.HasPrefix(line, "data: "):
			var access AccessLine
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &access) == nil &&
				access.LocalPort == port && !access.Time.Before(startedAt) {
				select {
				case activity <- access:
				case <-ctx.Done():
					return nil
				}
			}
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return scanner.Err()
}
