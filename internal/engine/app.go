package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/container"
	"github.com/iml885203/orbit/internal/env"
	"github.com/iml885203/orbit/internal/health"
	"github.com/iml885203/orbit/logging"
	"github.com/iml885203/orbit/port"
	"github.com/iml885203/orbit/process"
)

// App wires together all components and manages the full lifecycle.
type App struct {
	// Holder publishes the immutable config snapshots shared with the
	// daemon (Server.holder is the same object). Operations Load() once
	// each; construction-time scalars (CancelGrace, poller interval) are
	// deliberate startup snapshots.
	Holder          *config.Holder
	ContainerMgr    *container.Manager
	ProcessMgr      *process.Manager
	Orchestrator    *Orchestrator
	HealthChecker   *health.Checker // exposed so daemon server can read per-service health progress
	Logs            *logging.Multiplexer
	Poller          *container.Poller
	GetToggleStates func() map[string]bool   // injected by daemon server
	GetServiceModes func() map[string]string // injected by daemon server
	// TracingEndpoint returns the OTLP/HTTP base endpoint to inject into a
	// starting service, or "" when nothing should be injected (tracing off, or
	// the receiver never bound). Injected by the daemon server because only it
	// knows the port the receiver ACTUALLY bound after fallback — config alone
	// can be wrong once a conflict moved the port. nil (no daemon) means no
	// injection.
	TracingEndpoint      func() string
	runtimeReservedPorts []int

	// OnFatal, if set, receives unrecoverable background errors so the
	// owner can trigger graceful shutdown. Without it, deferred cleanup
	// (socket, PID file) would be skipped by os.Exit.
	OnFatal func(error)
	// OnExternalContainerRestart exposes lifecycle drift to the daemon so it
	// can persist and present the observation.
	OnExternalContainerRestart func(ExternalContainerRestart)
}

func (a *App) signalFatal(err error) {
	if a.OnFatal != nil {
		a.OnFatal(err)
		return
	}
	os.Exit(1)
}

// NewApp creates and wires all components. Caller must call Shutdown when done.
// detachedDeps is passed through to the orchestrator; pass nil if no edges are detached.
func NewApp(
	cfg *config.Config,
	serviceModes map[string]string,
	detachedDeps map[string][]string,
	namespace string,
	runtimeReservedPorts ...int,
) (*App, error) {
	containerMgr, err := container.NewManager(namespace)
	if err != nil {
		return nil, fmt.Errorf("initializing container manager: %w", err)
	}
	resolutions, err := port.ResolveAutoPorts(cfg, func(name string, target int) (int, bool, error) {
		return containerMgr.ManagedHostPort(context.Background(), name, target)
	}, runtimeReservedPorts...)
	if err != nil {
		_ = containerMgr.Close()
		return nil, fmt.Errorf("resolving automatic ports: %w", err)
	}
	for _, resolution := range resolutions {
		slog.Info(
			"selected available port",
			"component", "orbit",
			"name", resolution.Resource,
			"label", resolution.Label,
			"preferred", resolution.Preferred,
			"actual", resolution.Actual,
		)
	}

	if err := containerMgr.EnsureNetwork(context.Background()); err != nil {
		slog.Warn("failed to create Docker network", "component", "orbit", "err", err)
	}

	holder := config.NewHolder(cfg)
	processMgr := process.NewManager()
	processMgr.CancelGrace = cfg.Settings.ShutdownTimeout
	logs := logging.NewMultiplexer()
	healthChecker := health.NewChecker(logs, containerMgr)
	orch := NewOrchestrator(holder, serviceModes, detachedDeps)

	app := &App{
		Holder:               holder,
		ContainerMgr:         containerMgr,
		ProcessMgr:           processMgr,
		Orchestrator:         orch,
		HealthChecker:        healthChecker,
		Logs:                 logs,
		runtimeReservedPorts: append([]int(nil), runtimeReservedPorts...),
	}

	app.wireLogCapture(logs, orch)
	app.wireContainerCallbacks(containerMgr)
	app.wireProcessCallbacks(processMgr, holder)
	app.wireHealthCallbacks(healthChecker, holder, orch)
	orch.OnRunInit = containerMgr.RunInit
	app.Poller = app.newPoller(cfg, orch)

	return app, nil
}

func (a *App) PrepareConfig(cfg *config.Config) error {
	resolutions, err := port.ResolveAutoPorts(cfg, func(name string, target int) (int, bool, error) {
		return a.ContainerMgr.ManagedHostPort(context.Background(), name, target)
	}, a.runtimeReservedPorts...)
	if err != nil {
		return fmt.Errorf("resolving automatic ports: %w", err)
	}
	for _, resolution := range resolutions {
		slog.Info(
			"selected available port",
			"component", "orbit",
			"name", resolution.Resource,
			"label", resolution.Label,
			"preferred", resolution.Preferred,
			"actual", resolution.Actual,
		)
	}
	return nil
}

func (a *App) wireLogCapture(logs *logging.Multiplexer, orch *Orchestrator) {
	a.ContainerMgr.OnOutput = func(name, line string) { logs.Write(name, line) }
	a.ProcessMgr.OnOutput = func(name, line string) { logs.Write(name, line) }
	narrate := func(name, msg string) { logs.Write(name, "[orbit] "+msg) }
	a.ContainerMgr.OnAction = narrate
	a.ProcessMgr.OnAction = narrate
	orch.OnAction = narrate
	a.ProcessMgr.OnExit = func(name string, epoch int, err error, stderr []string) {
		msg := "exited"
		if err != nil {
			msg = fmt.Sprintf("exited: %v", err)
		}
		orch.Events() <- Event{
			Type:       EventProcessExited,
			Service:    name,
			Message:    msg,
			Evidence:   processFailureEvidence(err, stderr),
			Generation: epoch,
		}
	}
}

func processFailureEvidence(exitErr error, stderr []string) string {
	var processExit *exec.ExitError
	if errors.As(exitErr, &processExit) && processExit.ExitCode() < 0 {
		return ""
	}
	const maxLength = 240
	for i := len(stderr) - 1; i >= 0; i-- {
		line := strings.TrimSpace(stderr[i])
		if line == "" || strings.HasPrefix(line, "[orbit]") {
			continue
		}
		if len(line) > maxLength {
			return line[:maxLength-1] + "…"
		}
		return line
	}
	return ""
}

func (a *App) wireContainerCallbacks(mgr *container.Manager) {
	a.Orchestrator.OnStartContainer = func(ctx context.Context, name string, c *config.Container) error {
		slog.Info("starting container", "component", "orbit", "name", name)
		if err := mgr.Start(ctx, name, c); err != nil {
			return err
		}
		for i := range c.Sidecars {
			if err := mgr.StartSidecar(ctx, name, c, &c.Sidecars[i]); err != nil {
				slog.Warn("sidecar failed", "component", "orbit", "name", c.Sidecars[i].Name, "err", err)
			}
		}
		// Auto-configure sidecars that need initial setup
		go container.SetupSidecars(name, c)
		return nil
	}
	a.Orchestrator.OnStopContainer = func(ctx context.Context, name string) error {
		return mgr.Stop(ctx, name)
	}
}

func (a *App) wireProcessCallbacks(mgr *process.Manager, holder *config.Holder) {
	a.Orchestrator.OnStartProcess = func(ctx context.Context, name string, generation int, cfg *config.Config, svc *config.Service) error {
		slog.Info("starting service", "component", "orbit", "name", name)
		if err := ensureServicePortsAvailable(name, svc); err != nil {
			return err
		}
		var toggleStates map[string]bool
		if a.GetToggleStates != nil {
			toggleStates = a.GetToggleStates()
		}
		envVars := env.BuildEnv(svc, cfg, toggleStates)
		env.InjectServicePorts(envVars, svc.Ports)
		// Point services with no OTLP endpoint of their own at Orbit's receiver,
		// but stand aside for a service that already sets
		// OTEL_EXPORTER_OTLP_ENDPOINT — silently overwriting it would redirect
		// telemetry intended for an external collector.
		if a.TracingEndpoint != nil {
			if endpoint := a.TracingEndpoint(); endpoint != "" {
				env.InjectOTEL(envVars, name, endpoint)
			}
		}
		dir := svc.Path
		command := svc.Command
		if svc.Type == "dotnet" {
			dir = filepath.Dir(svc.Path)
			proj := filepath.Base(svc.Path)
			if svc.Watch {
				command = fmt.Sprintf("dotnet watch run --project %s", proj)
			} else {
				// Emit build started event
				a.Orchestrator.Events() <- Event{Type: EventBuildStarted, Service: name}

				// Build with streaming output to log multiplexer
				buildCmd := exec.CommandContext(ctx, "dotnet", "build", proj, "-v", "minimal")
				buildCmd.Dir = dir
				buildCmd.Env = append(os.Environ(), "DOTNET_CLI_UI_LANGUAGE=en")
				for k, v := range svc.BuildEnv {
					buildCmd.Env = append(buildCmd.Env, k+"="+v)
				}
				// Disable NuGet vulnerability audit by default — it requires
				// reaching every package source (including private feeds) on
				// every build, which fails offline / without VPN and surfaces
				// as NU1900 (warning-as-error). Set NuGetAudit=true to opt in.
				if os.Getenv("NuGetAudit") == "" {
					buildCmd.Env = append(buildCmd.Env, "NuGetAudit=false")
				}

				stdout, err := buildCmd.StdoutPipe()
				if err != nil {
					a.Orchestrator.Events() <- Event{Type: EventBuildFailed, Service: name}
					return fmt.Errorf("dotnet build pipe failed: %w", err)
				}
				stderr, err := buildCmd.StderrPipe()
				if err != nil {
					a.Orchestrator.Events() <- Event{Type: EventBuildFailed, Service: name}
					return fmt.Errorf("dotnet build pipe failed: %w", err)
				}
				if err := buildCmd.Start(); err != nil {
					a.Orchestrator.Events() <- Event{Type: EventBuildFailed, Service: name}
					return fmt.Errorf("dotnet build start failed: %w", err)
				}

				// Stream build output to logs
				go func() {
					scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
					for scanner.Scan() {
						a.Logs.Write(name, scanner.Text())
					}
				}()

				if err := buildCmd.Wait(); err != nil {
					a.Orchestrator.Events() <- Event{Type: EventBuildFailed, Service: name}
					return fmt.Errorf("dotnet build failed")
				}

				a.Orchestrator.Events() <- Event{Type: EventBuildComplete, Service: name}

				assemblyPath, err := resolveDotnetAssemblyPath(dir, proj)
				if err != nil {
					return err
				}
				command = fmt.Sprintf("dotnet %s", assemblyPath)
			}
		}
		return mgr.Start(ctx, name, dir, command, envVars, svc.PreStart, generation)
	}
	a.Orchestrator.OnStopProcess = func(name string) error {
		cfg := holder.Load()
		err := mgr.Stop(name, cfg.Settings.ShutdownTimeout)
		// Check for zombie processes still holding ports
		if svc, ok := cfg.Services[name]; ok {
			ports := make([]int, 0, len(svc.Ports))
			for _, p := range svc.Ports {
				ports = append(ports, p.Host)
			}
			if holders := process.FindPortHolders(ports); len(holders) > 0 {
				for _, h := range holders {
					slog.Warn("port still held after stop", "component", "process", "port", h.Port, "pid", h.PID, "name", name)
				}
			}
		}
		return err
	}
}

func ensureServicePortsAvailable(name string, svc *config.Service) error {
	ports := make([]int, 0, len(svc.Ports))
	for _, definition := range svc.Ports {
		ports = append(ports, definition.Host)
	}
	conflicts := port.CheckPorts(map[string][]int{name: ports})
	if len(conflicts) > 0 {
		return port.NewConflictError(conflicts[0])
	}
	return nil
}

func (a *App) wireHealthCallbacks(checker *health.Checker, holder *config.Holder, orch *Orchestrator) {
	a.Orchestrator.OnHealthCheck = func(ctx context.Context, name string, generation int) error {
		// Snapshot for this generation's whole health lifecycle: the
		// startup poll, zombie checks and recovery probing all read the
		// config the service was started with.
		cfg := holder.Load()
		info, _ := a.Orchestrator.GetServiceInfo(name)
		hc := readinessCheckForResource(cfg, name, info.Kind)
		managedProcess := info.Kind != "container"
		onProbeResult := func(r health.Result) {
			if !r.Healthy {
				return
			}
			if managedProcess {
				if !a.ProcessMgr.IsAlive(name) {
					slog.Warn("port responds but process is dead — zombie detected", "component", "health", "name", name)
					orch.Events() <- Event{
						Type:        EventHealthFail,
						Service:     name,
						Message:     "port responds but managed process is dead (zombie)",
						FailureKind: FailureKindProcess,
						Generation:  generation,
					}
					return
				}
			}
			orch.Events() <- Event{Type: EventHealthOK, Service: name, Generation: generation}
		}
		onRuntimeResult := func(r health.Result) {
			if r.Healthy {
				onProbeResult(r)
				return
			}
			orch.Events() <- Event{
				Type:        EventHealthFail,
				Service:     name,
				Message:     r.Message,
				FailureKind: FailureKindHealth,
				Generation:  generation,
			}
		}
		go func() {
			if hc == nil {
				timer := time.NewTimer(500 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					onProbeResult(health.Result{Service: name, Healthy: true, Message: "process remained running after startup"})
					return
				}
			}
			err := checker.WaitForHealthy(ctx, name, hc, onProbeResult)
			if err == nil {
				if health.SupportsRecovery(hc) {
					_ = checker.MonitorHealthy(ctx, name, hc, onRuntimeResult)
				}
				return
			}
			// Mark recovery BEFORE emitting the fail event: the event flips
			// the service to degraded, and a status poll landing between
			// "degraded" and "recovering" would read as terminal to CLI
			// waits. Announce-then-fail closes that window structurally.
			willRecover := ctx.Err() == nil && health.SupportsRecovery(hc)
			if willRecover {
				checker.MarkRecovering(name, generation)
			}
			orch.Events() <- Event{
				Type:        EventHealthFail,
				Service:     name,
				Message:     err.Error(),
				Err:         err,
				FailureKind: FailureKindHealth,
				Generation:  generation,
			}
			if !willRecover {
				return
			}
			// The budget ran out but the service may just be warming up
			// slower than the retries allowed — keep probing gently and
			// flip back to healthy on recovery instead of staying red
			// until someone runs a manual restart. Cancelled by the same
			// per-service ctx that stops the startup poll.
			slog.Info("health budget spent — probing for recovery", "component", "health", "name", name)
			if err := checker.RecoverHealthy(ctx, name, generation, hc, onProbeResult); err == nil {
				// onProbeResult may have vetoed the success as a zombie —
				// don't log a recovery the state machine rejected.
				if _, isSvc := cfg.Services[name]; isSvc && !a.ProcessMgr.IsAlive(name) {
					return
				}
				slog.Info("service recovered", "component", "health", "name", name)
				_ = checker.MonitorHealthy(ctx, name, hc, onRuntimeResult)
			}
		}()
		return nil
	}
}

func readinessCheckForResource(
	cfg *config.Config,
	name string,
	kind string,
) *config.HealthCheckConfig {
	if kind == "container" {
		if container, ok := cfg.Containers[name]; ok {
			return resourceReadinessCheck(
				container.HealthCheck,
				container.Ports,
				cfg.Settings.HealthCheckInterval,
			)
		}
		return nil
	}
	if service, ok := cfg.Services[name]; ok {
		return resourceReadinessCheck(
			service.HealthCheck,
			service.Ports,
			cfg.Settings.HealthCheckInterval,
		)
	}
	return nil
}

func resourceReadinessCheck(
	explicit *config.HealthCheckConfig,
	ports map[string]config.PortDef,
	interval time.Duration,
) *config.HealthCheckConfig {
	if explicit != nil {
		return explicit
	}
	port := 0
	if endpoint, ok := ports["http"]; ok {
		port = endpoint.Host
	} else if len(ports) == 1 {
		for _, endpoint := range ports {
			port = endpoint.Host
		}
	}
	if port == 0 {
		return nil
	}
	return &config.HealthCheckConfig{
		Type:             "tcp",
		Port:             port,
		Interval:         interval,
		Timeout:          5 * time.Second,
		Retries:          config.DefaultHealthRetries,
		FailureThreshold: config.DefaultHealthFailureThreshold,
	}
}

func (a *App) newPoller(cfg *config.Config, orch *Orchestrator) *container.Poller {
	poller := container.NewPoller(nil, a.ContainerMgr.Namespace(), cfg.Settings.DockerPollInterval)
	poller.OnUnavailable = func(_ error) {
		orch.OnContainerObservationUnavailable()
	}
	poller.OnStateUpdate = func(states map[string]container.ContainerState) {
		for name, state := range states {
			restart := orch.OnContainerObserved(name, state.Running, state.StartedAt)
			if restart != nil && a.OnExternalContainerRestart != nil {
				a.OnExternalContainerRestart(*restart)
			}
		}
		// Containers absent from the poll no longer exist in Docker at
		// all — reconcile those too, or a remove that outlives its stop
		// ctx leaves the service degraded forever.
		for _, svc := range orch.GetAllServices() {
			if svc.Kind != "container" {
				continue
			}
			if _, seen := states[svc.Name]; !seen {
				orch.OnContainerMissing(svc.Name)
			}
		}
	}
	return poller
}

// MarkInfraHealthy marks all containers as healthy (for when infra is already running).
func (a *App) MarkInfraHealthy() {
	a.Orchestrator.mu.Lock()
	defer a.Orchestrator.mu.Unlock()
	for name, info := range a.Orchestrator.services {
		if info.Kind == "container" {
			info.Transition(StateHealthy)
			// Mark this container ready in every service's PendingDeps.
			// We don't fire EventDepsReady here — the orchestrator's normal
			// Start() flow will discover the empty PendingDeps when called.
			for _, svcInfo := range a.Orchestrator.services {
				svcInfo.MarkDependencyReady(name)
			}
		}
	}
}

// Start launches the orchestrator and poller in background goroutines.
func (a *App) Start(ctx context.Context) {
	go a.Poller.Run(ctx)
	go func() {
		if err := a.Orchestrator.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("orchestrator fatal", "component", "orchestrator", "err", err)
			a.signalFatal(err)
		}
	}()
}

// Shutdown gracefully stops all processes and containers.
func (a *App) Shutdown() {
	a.ProcessMgr.StopAll(a.Holder.Load().Settings.ShutdownTimeout)
	a.warnZombiePorts()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), a.Holder.Load().Settings.ShutdownTimeout)
	defer stopCancel()
	_ = a.ContainerMgr.StopAll(stopCtx)
	_ = a.ContainerMgr.Close()
}

// ShutdownServices stops all processes but keeps containers running.
func (a *App) ShutdownServices() {
	a.ProcessMgr.StopAll(a.Holder.Load().Settings.ShutdownTimeout)
	a.warnZombiePorts()
}

// StopServicesOnly stops every service in parallel through the orchestrator
// lifecycle (so the graph sees stopping → stopped transitions), but leaves
// containers running. Used by env switch — infra is shared across envs and
// tearing it down on every switch adds painful seconds of cold start.
// Blocks until every goroutine returns.
func (a *App) StopServicesOnly() {
	services := a.Orchestrator.GetAllServices()
	names := make([]string, 0, len(services))
	for i := range services {
		if services[i].Kind != "service" {
			continue
		}
		names = append(names, services[i].Name)
	}
	a.StopServices(names)
}

// StopServices gives every selected resource its own shutdown deadline so one
// slow process cannot consume the budget of otherwise independent resources.
func (a *App) StopServices(names []string) {
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), a.Holder.Load().Settings.ShutdownTimeout)
			defer cancel()
			if err := a.StopService(ctx, name); err != nil {
				slog.Error("stop failed", "component", "stop-selected", "name", name, "err", err)
			}
		}(name)
	}
	wg.Wait()
}

func (a *App) StopServicesForConfig(names []string) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(names))
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), a.Holder.Load().Settings.ShutdownTimeout)
			defer cancel()
			if err := a.StopService(ctx, name); err != nil {
				errs <- fmt.Errorf("stopping %s: %w", name, err)
			}
		}(name)
	}
	wg.Wait()
	close(errs)
	var failures []error
	for err := range errs {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

// warnZombiePorts logs warnings about zombie processes still holding service ports.
func (a *App) warnZombiePorts() {
	var ports []int
	for _, svc := range a.Holder.Load().Services {
		for _, p := range svc.Ports {
			ports = append(ports, p.Host)
		}
	}
	if len(ports) == 0 {
		return
	}
	for _, h := range process.FindPortHolders(ports) {
		slog.Warn("port still held after shutdown", "component", "process", "port", h.Port, "pid", h.PID)
	}
}

// StartServices transitions the listed services from stopped/degraded to starting,
// respecting dependencies. Unknown names are ignored.
func (a *App) StartServices(names []string) {
	a.Orchestrator.Start(names)
}

// StopService stops a single service or container.
func (a *App) StopService(ctx context.Context, name string) error {
	return a.Orchestrator.StopService(ctx, name)
}

// StopAllServices stops every known service and container in parallel by
// routing each through StopService, so each goes through the
// stopping → stopped lifecycle that status pollers can render. Each
// service gets its own deadline derived from the configured
// ShutdownTimeout, so a slow service does not steal budget from
// services that would otherwise stop quickly. Blocks until every
// goroutine returns.
func (a *App) StopAllServices() {
	services := a.Orchestrator.GetAllServices()
	names := make([]string, 0, len(services))
	for i := range services {
		names = append(names, services[i].Name)
	}
	a.StopServices(names)

	// Safety net: sweep any orbit-managed containers in this namespace that
	// the orchestrator does not track (e.g. orphaned sidecars from a prior
	// daemon run, or a parent that was already gone when its sidecar was
	// created). Without this, `orbit down` can leave UI containers like
	// dbgate / kafka-ui running.
	sweepCtx, cancel := context.WithTimeout(context.Background(), a.Holder.Load().Settings.ShutdownTimeout)
	defer cancel()
	if err := a.ContainerMgr.StopAll(sweepCtx); err != nil {
		slog.Warn("container sweep failed", "component", "stop-all", "err", err)
	}
}

// RestartService restarts a single service or container.
func (a *App) RestartService(ctx context.Context, name string) error {
	return a.Orchestrator.RestartService(ctx, name)
}

// ReconcilePersistedProcesses safely retires host processes left by an abrupt
// daemon exit. Unlike containers, a host process still owns stdout/stderr
// pipes connected to the dead daemon and can exit later on a broken pipe;
// treating it as healthy would make `orbit up` report a false recovery.
// Persisted PID/PGID proves ownership, so Orbit can stop it without asking the
// user to inspect or kill anything, then normal startup creates a fresh child.
func (a *App) ReconcilePersistedProcesses(processes map[string]struct{ PID, PGID int }) error {
	for name, p := range processes {
		if err := a.ProcessMgr.Reconnect(name, p.PID, p.PGID); err != nil {
			slog.Warn("reconnect failed, marking service stopped", "component", "orbit", "name", name, "err", err)
			a.Orchestrator.MarkServiceStopped(name)
			continue
		}
		if err := a.ProcessMgr.Stop(name, a.Holder.Load().Settings.ShutdownTimeout); err != nil {
			return fmt.Errorf("stopping persisted process %s: %w", name, err)
		}
		a.Orchestrator.MarkServiceStopped(name)
		slog.Info("retired persisted process", "component", "orbit", "name", name, "pid", p.PID)
	}
	return nil
}

// ShutdownTimeout returns the configured shutdown timeout.
func (a *App) ShutdownTimeout() time.Duration {
	return a.Holder.Load().Settings.ShutdownTimeout
}
