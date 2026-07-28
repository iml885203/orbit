package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/logging"
	"github.com/iml885203/orbit/platform"
	"github.com/spf13/cobra"
)

var daemonRestartDelay time.Duration

func runDaemonStart(_ *cobra.Command, _ []string) error {
	if err := ensureDaemonStarted(configFile); err != nil {
		return err
	}
	if cli.JSONOutput {
		pid, alive := daemon.IsDaemonRunning()
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildDaemonJSONData(daemonJSONOptions{
			Operation:  "daemon_start",
			Running:    alive,
			PID:        pid,
			ConfigPath: configFile,
			Dashboard:  fmt.Sprintf("http://localhost:%d", daemon.DashboardPort()),
		}), []cli.JSONAction{cli.StatusAction()})
	}
	fmt.Printf("Daemon running. Dashboard: http://localhost:%d\n", daemon.DashboardPort())
	return nil
}

func runDaemonRestart(cmd *cobra.Command, args []string) error {
	if daemonRestartDelay > 0 {
		time.Sleep(daemonRestartDelay)
	}
	previousPID, alive := daemon.IsDaemonRunning()
	stopMethod, pid, running, err := restartDaemon(configFile, previousPID, alive)
	if err != nil {
		return err
	}
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildDaemonJSONData(daemonJSONOptions{
			Operation:                "daemon_restart",
			Running:                  running,
			PID:                      pid,
			PreviousPID:              previousPID,
			ConfigPath:               configFile,
			Dashboard:                fmt.Sprintf("http://localhost:%d", daemon.DashboardPort()),
			RequestedServiceShutdown: alive,
			StopMethod:               stopMethod,
		}), []cli.JSONAction{cli.StatusAction()})
	}
	fmt.Printf("Daemon running. Dashboard: http://localhost:%d\n", daemon.DashboardPort())
	return nil
}

func launchDashboardEnvRestart(configPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding orbit executable: %w", err)
	}
	logFile, err := os.OpenFile(daemon.DefaultLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening daemon log: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	cmd := exec.Command(exe, "daemon", "restart", "--config", configPath, "--handoff-delay", "250ms")
	cmd.Dir = filepath.Dir(configPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	platform.DetachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting replacement process: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

func ensureDaemonStarted(configPath string) error {
	_, err := daemon.EnsureDaemon(configPath, groups)
	return renderDaemonStartError(err)
}

func restartDaemon(configPath string, previousPID int, alive bool) (daemonStopMethod, int, bool, error) {
	stopMethod := daemonStopNotRunning
	if alive {
		var err error
		stopMethod, err = stopDaemon(previousPID)
		if err != nil {
			return stopMethod, 0, false, err
		}
	}
	if err := ensureDaemonStarted(configPath); err != nil {
		return stopMethod, 0, false, err
	}
	pid, running := daemon.IsDaemonRunning()
	return stopMethod, pid, running, nil
}

type daemonStatusJSON struct {
	Running         bool   `json:"running"`
	PID             int    `json:"pid,omitempty"`
	Version         string `json:"version,omitempty"`
	OnDisk          string `json:"on_disk,omitempty"`
	OnDiskPath      string `json:"on_disk_path,omitempty"`
	UpdateAvailable bool   `json:"update_available,omitempty"`
	ConfigPath      string `json:"config_path,omitempty"`
	Dashboard       string `json:"dashboard,omitempty"`
}

func runDaemonStatus(_ *cobra.Command, _ []string) error {
	pid, alive := daemon.IsDaemonRunning()
	out := daemonStatusJSON{Running: alive, PID: pid}

	if alive {
		client := daemon.NewClient(daemon.DefaultSocketPath())
		if v, err := client.Version(); err == nil {
			out.Version = v.Running
			out.OnDisk = v.OnDisk
			out.OnDiskPath = v.OnDiskPath
			out.UpdateAvailable = v.UpdateAvailable
		}
		out.ConfigPath = resolveConfigFile()
		out.Dashboard = fmt.Sprintf("http://localhost:%d", daemon.DashboardPort())
	}

	if cli.JSONOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	if !alive {
		fmt.Println("Daemon not running.")
		return nil
	}
	fmt.Printf("Daemon    running (pid %d)\n", out.PID)
	if out.Version != "" {
		fmt.Printf("Version   %s\n", out.Version)
	}
	if out.UpdateAvailable && out.OnDisk != "" {
		location := out.OnDisk
		if out.OnDiskPath != "" {
			location = fmt.Sprintf("%s at %s", out.OnDisk, out.OnDiskPath)
		}
		fmt.Printf("          ⚠ newer orbit %s — orbit daemon restart\n", location)
	}
	if out.ConfigPath != "" {
		fmt.Printf("Config    %s\n", out.ConfigPath)
	}
	if out.Dashboard != "" {
		fmt.Printf("Dashboard %s\n", out.Dashboard)
	}
	return nil
}

func runDaemonStop(_ *cobra.Command, _ []string) error {
	pid, alive := daemon.IsDaemonRunning()
	if !alive {
		if cli.JSONOutput {
			return cli.WriteJSONSuccess(os.Stdout, commandString(), buildDaemonJSONData(daemonJSONOptions{
				Operation: "daemon_stop",
				Running:   false,
			}), nil)
		}
		fmt.Println("No daemon running.")
		return nil
	}

	stopMethod, err := stopDaemon(pid)
	if err != nil {
		return err
	}
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildDaemonJSONData(daemonJSONOptions{
			Operation:                "daemon_stop",
			Running:                  false,
			PreviousPID:              pid,
			RequestedServiceShutdown: true,
			StopMethod:               stopMethod,
		}), nil)
	}
	return nil
}

func stopDaemon(pid int) (daemonStopMethod, error) {
	client := daemon.NewClient(daemon.DefaultSocketPath())

	// Ask for graceful shutdown without waiting on a potentially stuck daemon.
	// Escalation inside waitForDaemonStop handles the case where the goroutine
	// hangs, never returns, or the daemon never ACKs.
	downDone := make(chan error, 1)
	go func() {
		_, err := client.Down(true)
		downDone <- err
	}()
	select {
	case err := <-downDone:
		if err != nil {
			_ = platform.SendTermSignal(pid)
		}
	case <-time.After(3 * time.Second):
		_ = platform.SendTermSignal(pid)
	}

	return waitForDaemonStop(pid)
}

type daemonStopMethod string

const (
	daemonStopNotRunning daemonStopMethod = ""
	daemonStopGraceful   daemonStopMethod = "graceful"
	daemonStopTerminated daemonStopMethod = "terminated"
	daemonStopKilled     daemonStopMethod = "killed"
)

type daemonJSONOptions struct {
	Operation                string
	Running                  bool
	PID                      int
	PreviousPID              int
	ConfigPath               string
	Dashboard                string
	RequestedServiceShutdown bool
	StopMethod               daemonStopMethod
}

type daemonJSONData struct {
	Operation                string `json:"operation"`
	Running                  bool   `json:"running"`
	PID                      int    `json:"pid,omitempty"`
	PreviousPID              int    `json:"previous_pid,omitempty"`
	ConfigPath               string `json:"config_path,omitempty"`
	Dashboard                string `json:"dashboard,omitempty"`
	RequestedServiceShutdown bool   `json:"requested_service_shutdown"`
	StopMethod               string `json:"stop_method,omitempty"`
}

func buildDaemonJSONData(opts daemonJSONOptions) daemonJSONData {
	return daemonJSONData{
		Operation:                opts.Operation,
		Running:                  opts.Running,
		PID:                      opts.PID,
		PreviousPID:              opts.PreviousPID,
		ConfigPath:               opts.ConfigPath,
		Dashboard:                opts.Dashboard,
		RequestedServiceShutdown: opts.RequestedServiceShutdown,
		StopMethod:               string(opts.StopMethod),
	}
}

func runDaemon(_ *cobra.Command, _ []string) error {
	logFile, err := daemon.RedirectLogToFile()
	if err != nil {
		return fmt.Errorf("opening daemon log: %w", err)
	}
	log.SetOutput(logFile)
	logging.SetupDefault(logFile, "ORBIT_LOG_LEVEL")
	defer func() { _ = logFile.Close() }()

	if err := daemon.WritePID(); err != nil {
		return fmt.Errorf("writing PID file: %w", err)
	}
	defer daemon.Cleanup()

	slog.Info("starting", "component", "daemon", "pid", os.Getpid(), "config", configFile)

	settings := daemon.LoadSettings(daemon.DefaultSettingsPath())
	settings.ApplyToEnv()

	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	// The single Holder shared by the engine and the daemon — every reader
	// Loads immutable snapshots from here, both writers publish through it.
	holder := config.NewHolder(cfg)

	// Detached edges are scoped per env (basename of the config path
	// without extension), matching how the daemon handler keys them in
	// settings.json. Without this lookup the orchestrator would never
	// see the user's detach choices.
	envName := strings.TrimSuffix(filepath.Base(configFile), filepath.Ext(configFile))
	detachedDeps := settings.GetDetachedEdges(envName)

	app, err := engine.NewApp(holder, settings.GetServiceModes(), detachedDeps, os.Getenv("ORBIT_NAMESPACE"))
	if err != nil {
		return fmt.Errorf("creating app: %w", err)
	}

	stateFile := daemonsrv.NewStateFile(daemonsrv.DefaultStatePath())

	prevState, err := stateFile.Read()
	if err == nil && len(prevState.Processes) > 0 {
		procs := make(map[string]struct{ PID, PGID int })
		for name, rec := range prevState.Processes {
			procs[name] = struct{ PID, PGID int }{rec.PID, rec.PGID}
		}
		app.Reconnect(procs)
	}

	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	app.GetToggleStates = func() map[string]bool {
		return settings.GetEnvToggles()
	}
	app.GetServiceModes = func() map[string]string {
		return settings.GetServiceModes()
	}

	server := daemonsrv.NewServer(app, holder, stateFile, settings, buildVersion(), dashboardFS, extensions)
	server.SetConfigPath(configFile)
	server.SetRestartLauncher(launchDashboardEnvRestart)
	// Point starting services at the receiver's actual bound endpoint. Reads
	// live receiver state at start time, so a service launched after a port
	// fallback gets the real port. Wired here (not in NewApp) because the
	// endpoint is the server's to know.
	app.TracingEndpoint = server.TracingEndpoint

	// Route orchestrator fatals through graceful shutdown so defer cleanup
	// (socket, PID) runs instead of os.Exit skipping it.
	app.OnFatal = func(_ error) {
		server.PersistState()
		cancel()
	}
	app.Start(ctx)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down gracefully", "component", "daemon", "signal", sig.String())
		server.PersistState()
		cancel()

		sig = <-sigCh
		slog.Warn("received signal again, forcing exit", "component", "daemon", "signal", sig.String())
		os.Exit(1)
	}()

	eventSub, unsubscribeEvents := app.Orchestrator.Subscribe()
	defer unsubscribeEvents()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-eventSub:
				if !ok {
					return
				}
				switch evt.Type {
				case engine.EventHealthOK, engine.EventProcessExited, engine.EventHealthFail:
					server.PersistState()
				}
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				server.PersistState()
			}
		}
	}()

	slog.Info("ready", "component", "daemon")
	return server.ListenAndServe(ctx, cancel)
}

// waitForDaemonStop polls the daemon process directly (signal 0) and
// escalates to SIGTERM then SIGKILL when graceful shutdown takes too long.
// We avoid the HTTP client here because a frozen daemon would make each
// probe block on the client's request timeout.
func waitForDaemonStop(pid int) (daemonStopMethod, error) {
	const (
		termAfter = 10 * time.Second
		killAfter = 20 * time.Second
		giveUp    = 25 * time.Second
	)
	start := time.Now()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	termSent, killSent := false, false
	for {
		<-ticker.C
		if !platform.IsProcessAlive(pid) {
			switch {
			case killSent:
				printDaemonStopResult(os.Stdout, daemonStopKilled)
				return daemonStopKilled, nil
			case termSent:
				printDaemonStopResult(os.Stdout, daemonStopTerminated)
				return daemonStopTerminated, nil
			default:
				printDaemonStopResult(os.Stdout, daemonStopGraceful)
				return daemonStopGraceful, nil
			}
		}
		elapsed := time.Since(start)
		switch {
		case elapsed > giveUp:
			return daemonStopNotRunning, cli.NewTimeoutError(fmt.Sprintf("timeout waiting for daemon to stop (pid %d still alive)", pid))
		case elapsed > killAfter && !killSent:
			_ = platform.SendKillSignal(pid)
			killSent = true
		case elapsed > termAfter && !termSent:
			_ = platform.SendTermSignal(pid)
			termSent = true
		}
	}
}

func printDaemonStopResult(w io.Writer, method daemonStopMethod) {
	if cli.JSONOutput {
		return
	}
	switch method {
	case daemonStopKilled:
		_, _ = fmt.Fprintln(w, "Daemon force-killed (graceful shutdown hung).")
	case daemonStopTerminated:
		_, _ = fmt.Fprintln(w, "Daemon stopped (after SIGTERM).")
	default:
		_, _ = fmt.Fprintln(w, "Daemon stopped.")
	}
}
