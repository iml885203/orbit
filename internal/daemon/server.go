package daemon

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/orbit/instance"
	"github.com/iml885203/orbit/internal/cmdmap/gaps"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/internal/history"
	"github.com/iml885203/orbit/internal/tracing"
)

// Server is the daemon HTTP server that listens on a unix socket.
type Server struct {
	app *engine.App
	// holder publishes immutable config snapshots, shared with the engine.
	// Readers Load() one snapshot per operation; the two writers
	// (restartSQLServer, handleEnvSwitch) serialize through UpdateConfig
	// (config_write.go), the sole acquirer of configWriteMu —
	// Load→build→Store is a read-modify-write and unserialized writers
	// would lose updates (e.g. a SQL restart rolling back an env switch).
	holder                  *config.Holder
	configWriteMu           sync.Mutex
	environmentTransitionMu sync.RWMutex
	environmentGeneration   atomic.Uint64
	background              sync.WaitGroup
	settings                *Settings
	// extHooks holds the registered feature seams (SSE event sources,
	// daemon-exit hooks). Appended in NewServer and ListenAndServe's
	// route setup — all before the listener serves — and read-only once
	// serving, so no lock.
	extHooks extension.DaemonHooks
	// extensions are the registered feature sets; DaemonSetup runs in
	// ListenAndServe's route setup. resourceContributors is append-only
	// during that setup, read-only once serving.
	extensions                 []extension.Extension
	resourceContributors       []ResourceContributor
	settingsPUTHooks           []SettingsPUTHook
	doctorContributors         []func() []DoctorCheck
	configPath                 string
	environmentContextKind     string
	environmentContextIdentity string
	baseline                   configBaseline
	pathMu                     sync.RWMutex // guards config path, environment context, and baseline
	// engineStale closes the handoff window after an API env switch publishes
	// new config but before the replacement daemon rebuilds the service graph.
	engineStale atomic.Bool
	// restartLauncher crosses the daemon/app package boundary because only the
	// assembled binary knows how to replace itself on every supported OS.
	restartLauncher func(string, string) error
	startedAt       time.Time
	stateFile       *StateFile
	history         *history.Recorder
	gaps            *gaps.Collector
	listener        net.Listener
	httpServer      *http.Server
	cancelFunc      context.CancelFunc // cancels the app context
	baseCtx         context.Context
	version         string
	instanceName    string
	tracing         *tracing.Store
	// staticFS holds the dashboard assets (already rooted at the dist
	// contents); nil when the build embeds none. See staticHandler.
	staticFS fs.FS
}

func (s *Server) startBackground(work func()) {
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		work()
	}()
}

func (s *Server) startEnvironmentBackground(generation uint64, work func()) {
	s.startBackground(func() {
		s.environmentTransitionMu.Lock()
		defer s.environmentTransitionMu.Unlock()
		if s.environmentGeneration.Load() != generation {
			return
		}
		work()
	})
}

func (s *Server) waitForBackground() {
	s.background.Wait()
}

// NewServer creates a new daemon server. version is the build string
// reported via /api/version. ui is the dashboard asset tree (rooted at
// the dist contents; nil for a build without one — see staticHandler).
// Extension state (routes, SSE sources, OnDown hooks) is constructed by
// DaemonSetup during ListenAndServe's route setup, NOT here — a Server
// that never serves has no feature hooks, so exercising handleDown
// without ListenAndServe skips e.g. tunnel release (the remote TTL
// covers that path, same as raw signals).
func NewServer(app *engine.App, holder *config.Holder, stateFile *StateFile, settings *Settings, version string, ui fs.FS, exts []extension.Extension) *Server {
	cfg := holder.Load()
	srv := &Server{
		app:          app,
		holder:       holder,
		settings:     settings,
		startedAt:    time.Now(),
		stateFile:    stateFile,
		version:      version,
		instanceName: instance.CurrentName(),
		extensions:   exts,
		tracing:      tracing.NewStore(cfg.TracingMaxTraces()),
		staticFS:     ui,
	}
	return srv
}

func (s *Server) SetRestartLauncher(launcher func(string, string) error) {
	s.restartLauncher = launcher
}

// ListenAndServe starts the HTTP server on both unix socket and TCP (dashboard).
func (s *Server) ListenAndServe(ctx context.Context, cancel context.CancelFunc) error {
	s.cancelFunc = cancel
	s.baseCtx = ctx
	sockPath := DefaultSocketPath()

	if err := ValidateSocketPath(sockPath); err != nil {
		return err
	}
	// Binding a unix socket does not create its parent. Today the daemon opens
	// its log first, which creates the home, so this is a second line of
	// defence rather than the only one — kept because a bind failure here
	// surfaces as a bare "invalid argument" that says nothing about a missing
	// directory.
	if _, err := EnsureOrbitDir(); err != nil {
		return err
	}

	_ = os.Remove(sockPath)

	unixLn, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", sockPath, err)
	}
	s.listener = unixLn
	if err := os.Chmod(sockPath, 0600); err != nil {
		_ = unixLn.Close()
		return fmt.Errorf("restricting daemon socket permissions: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/up", s.handleUp)
	mux.HandleFunc("/api/down", s.handleDown)
	mux.HandleFunc("/api/stop/", s.handleStop)
	mux.HandleFunc("/api/restart/", s.handleRestart)
	mux.HandleFunc("/api/logs/", s.handleLogs)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/doctor", s.handleDoctor)
	mux.HandleFunc("/api/env-toggles", s.handleEnvToggles)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/service-mode/", s.handleServiceMode)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/version/restart", s.handleVersionRestart)
	mux.HandleFunc("/api/envs", s.handleEnvs)
	mux.HandleFunc("/api/envs/current", s.handleEnvSwitch)
	mux.HandleFunc("/api/sources", s.handleSources)
	mux.HandleFunc("/api/env/reconcile", s.handleEnvironmentReconcile)
	mux.HandleFunc("/api/graph", s.handleGraph)
	mux.HandleFunc("/api/resources", s.handleResources)
	mux.HandleFunc("/api/edges/", s.handleEdgeDetach)
	mux.HandleFunc("/api/service-env/", s.handleServiceEnv)
	if rec, err := history.New(OrbitDir()); err != nil {
		slog.Error("history recorder unavailable", "component", "daemon", "err", err)
	} else {
		s.history = rec
		s.gaps = gaps.New(filepath.Join(OrbitDir(), "gaps.json"))
		registerHistoryHandlers(mux, s.history, s.gaps)
		defer func() { _ = s.history.Close() }()
	}
	// Extension daemon setup: routes + hooks (feature SSE sources come
	// from DaemonSetup), still
	// before any listener serves — the read-only-once-serving discipline
	// on extHooks, resourceContributors, and settingsPUTHooks depends on
	// this spot.
	for _, ext := range s.extensions {
		if ext.DaemonSetup == nil {
			continue
		}
		s.extHooks.Merge(ext.DaemonSetup(s, mux))
	}
	registerTracingHandlers(mux, s)
	mux.Handle("/", s.staticHandler())

	s.httpServer = &http.Server{
		Handler: dashboardAccessMiddleware(HistoryMiddleware(s.history, s.gaps)(mux)),
	}

	slog.Info("listening", "component", "daemon", "socket", sockPath)

	// Dashboard TCP listener. A conflict here means another orbit (or
	// anything else) already owns the port; refusing to start is the
	// only way the CLI can surface that — a half-started daemon with no
	// dashboard is worse than no daemon.
	tcpLn, err := ListenDashboard(DashboardPort())
	if err != nil {
		_ = unixLn.Close() // unlinks the socket file
		return err
	}
	slog.Info("dashboard listening", "component", "daemon", "url", "http://"+tcpLn.Addr().String())
	go func() {
		if err := s.httpServer.Serve(tcpLn); err != nil && err != http.ErrServerClosed {
			slog.Error("dashboard listener error", "component", "daemon", "err", err)
		}
	}()

	// OTLP/HTTP receiver — only when this env has tracing on (default-on; see
	// config.TracingEnabled). A bind failure is non-fatal: tracing is an
	// observability surface, not a reason to refuse the whole daemon. It binds
	// loopback only. The real bind outcome is recorded on the store via
	// SetReceiver so /api/tracing/status reports whether the receiver is
	// actually live (and on which port after fallback), not just config intent.
	if cfg := s.holder.Load(); cfg.TracingEnabled() {
		s.startOTLPReceiver(ctx, cfg)
	} else {
		s.tracing.SetReceiver(false, 0, "")
	}

	go func() {
		<-ctx.Done()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = s.httpServer.Shutdown(shutCtx)
	}()

	serveErr := s.httpServer.Serve(unixLn)
	if serveErr != nil && serveErr != http.ErrServerClosed {
		return serveErr
	}
	// NOTE: extension teardown (e.g. tunnel claim release) on shutdown is
	// done in handleDown (req.All), BEFORE the daemon ctx is cancelled, so
	// releases reach the gateway promptly. The raw-signal path (Ctrl-C
	// straight to the daemon) skips handleDown — there we fall back to the
	// gateway's lease expiry instead of racing teardown against the CLI's
	// SIGKILL timer.
	return nil
}

// TracingEndpoint returns the OTLP/HTTP base endpoint services should export
// to, or "" when nothing should be injected (tracing off, or the receiver
// never bound). It reads the receiver's ACTUAL bound port from the store, so a
// service that starts after a port fallback is pointed at the real port — not
// the configured one. Wired into engine.App.TracingEndpoint by the daemon cmd.
func (s *Server) TracingEndpoint() string {
	st := s.tracing.Stats()
	if !st.ReceiverHealthy || st.ActualPort == 0 {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", st.ActualPort)
}

// otlpPortFallbackTries is how many consecutive ports the receiver probes from
// the implicit default before giving up. Only used when the port is NOT pinned
// in config (see config.TracingPortExplicit) — a pinned port binds once.
const otlpPortFallbackTries = 10

// bindOTLPListener probes up to tries consecutive loopback ports starting at
// desired, returning the first that binds along with its port. tries==1 pins
// the port (no fallback). It returns an error only when every attempt failed —
// the error is the last bind failure, or a range error if the walk ran past
// 65535. Pure and side-effect-free apart from the listener it opens, so the
// port policy can be tested without a running server.
func bindOTLPListener(desired, tries int) (net.Listener, int, error) {
	var lastErr error
	for i := 0; i < tries; i++ {
		port := desired + i
		if port > 65535 {
			break
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			lastErr = err
			continue
		}
		return ln, port, nil
	}
	if lastErr != nil {
		return nil, 0, lastErr
	}
	return nil, 0, fmt.Errorf("no port available from %d", desired)
}

// startOTLPReceiver binds the loopback OTLP/HTTP listener and serves it in the
// background, recording the outcome on the store via SetReceiver.
//
// Port policy: a pinned port (config set otlp_port) is bound exactly once — a
// conflict there is the user's to resolve, so it surfaces as a receiver error
// rather than silently moving. An implicit (default) port probes forward up to
// otlpPortFallbackTries so a stray process on 4318 doesn't silently disable
// tracing; the port that actually bound is reported via status.
func (s *Server) startOTLPReceiver(ctx context.Context, cfg *config.Config) {
	desired := cfg.TracingOTLPPort()
	tries := otlpPortFallbackTries
	if cfg.TracingPortExplicit() {
		tries = 1
	}

	otlpLn, boundPort, err := bindOTLPListener(desired, tries)
	if err != nil {
		slog.Warn("otlp receiver disabled", "component", "tracing", "desiredPort", desired, "err", err)
		s.tracing.SetReceiver(false, 0, err.Error())
		return
	}

	s.tracing.SetReceiver(true, boundPort, "")
	otlpSrv := &http.Server{Handler: s.tracing.OTLPHandler()}
	slog.Info("otlp receiver listening", "component", "tracing", "addr", otlpLn.Addr().String())
	go func() {
		if err := otlpSrv.Serve(otlpLn); err != nil && err != http.ErrServerClosed {
			slog.Error("otlp receiver error", "component", "tracing", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = otlpSrv.Shutdown(shutCtx)
	}()
}

// PersistState writes the current state to the state file.
func (s *Server) PersistState() {
	state := s.buildState()
	if err := s.stateFile.Write(state); err != nil {
		slog.Error("failed to persist state", "component", "daemon", "err", err)
	}
}

// buildState constructs the current DaemonState from the app.
func (s *Server) buildState() *DaemonState {
	configPath := s.ConfigPath()
	state := &DaemonState{
		ConfigPath: configPath,
		StartedAt:  s.startedAt,
		Processes:  make(map[string]ProcessRecord),
		Services:   make(map[string]ServiceStateEntry),
	}

	// Snapshot service states against one immutable config snapshot.
	snapshot := s.app.Orchestrator.GetAllServices()
	cfg := s.holder.Load()
	for i := range snapshot {
		svc := &snapshot[i]
		state.Services[svc.Name] = ServiceStateEntry{
			Kind:                  svc.Kind,
			State:                 svc.State.String(),
			ContainerStartedAt:    svc.ContainerStartedAt,
			ExternalRestartCount:  svc.ExternalRestartCount,
			LastExternalRestart:   svc.LastExternalRestart,
			LastExternalStartedAt: svc.LastExternalStartedAt,
		}

		// Record process info for services
		if svc.Kind == "service" {
			if pid, pgid, ok := s.app.ProcessMgr.GetProcessInfo(svc.Name); ok {
				rec := ProcessRecord{PID: pid, PGID: pgid}
				if svcCfg, ok := cfg.Services[svc.Name]; ok {
					rec.Command = svcCfg.Command
					rec.Dir = svcCfg.Path
				}
				state.Processes[svc.Name] = rec
			}
		}
	}

	return state
}

// RecordExternalContainerRestart makes an out-of-band Docker action visible
// in the same timeline users inspect for Orbit commands.
func (s *Server) RecordExternalContainerRestart(restart engine.ExternalContainerRestart) {
	s.PersistState()
	if s.history == nil {
		return
	}
	s.history.Record(history.Record{
		Timestamp: restart.ObservedAt,
		Source:    history.SourceSystem,
		Summary:   fmt.Sprintf("%s restarted outside Orbit", restart.Name),
		Status:    history.StatusOK,
	})
}

func (s *Server) epoch() int64 {
	return s.startedAt.UnixMilli()
}

// SetConfigPath stores the config path in the server for state persistence.
func (s *Server) SetConfigPath(path string) {
	s.pathMu.Lock()
	s.configPath = path
	s.environmentContextIdentity = canonicalEnvironmentPath(path)
	s.recordConfigBaselineLocked(path)
	s.pathMu.Unlock()
	s.environmentGeneration.Add(1)
}

func (s *Server) ConfigPath() string {
	s.pathMu.RLock()
	defer s.pathMu.RUnlock()
	return s.configPath
}

func (s *Server) SetEnvironmentContext(path, kind string) {
	identity := canonicalEnvironmentPath(path)
	if kind == "managed" {
		if managedIdentity, ok := managedEnvironmentIdentity(path); ok {
			identity = managedIdentity
		}
	}
	s.pathMu.Lock()
	s.configPath = path
	s.environmentContextKind = kind
	s.environmentContextIdentity = identity
	s.recordConfigBaselineLocked(path)
	s.pathMu.Unlock()
	s.environmentGeneration.Add(1)
}

func (s *Server) environmentContext() EnvironmentContext {
	s.pathMu.RLock()
	configPath := s.configPath
	kind := s.environmentContextKind
	identity := s.environmentContextIdentity
	s.pathMu.RUnlock()

	if identity == "" {
		identity = canonicalEnvironmentPath(configPath)
	}
	selectedPath := canonicalEnvironmentPath(ReadCurrentEnv())
	selectedIdentity, _ := managedEnvironmentIdentity(selectedPath)
	if kind == "" {
		if selectedPath != "" && (selectedPath == identity || selectedIdentity == identity) {
			kind = "managed"
		} else {
			kind = "explicit"
		}
	}
	displayName := EnvShortName(configPath)
	projectRoot := ""
	if kind == "project" {
		projectRoot = filepath.Dir(configPath)
		displayName = filepath.Base(projectRoot)
	}
	context := EnvironmentContext{
		Kind:        kind,
		Identity:    identity,
		DisplayName: displayName,
		ConfigPath:  configPath,
		ProjectRoot: projectRoot,
		Available:   configFileAvailable(configPath),
		Running:     s.runningCount() > 0,
	}
	if selectedPath != "" {
		context.ManagedSelection = &ManagedEnvironmentSelection{
			Identity: selectedIdentity,
			Name:     EnvShortName(selectedPath),
			Path:     selectedPath,
			Active:   kind == "managed" && (selectedPath == identity || selectedIdentity == identity),
		}
	}
	return context
}

func managedEnvironmentIdentity(path string) (string, bool) {
	registry, err := loadEnvironmentSourceRegistry()
	if err != nil {
		return "", false
	}
	_, identity, found := registry.SourceForPath(OrbitDir(), path)
	return identity, found
}

func canonicalEnvironmentPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

func configFileAvailable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	_, err = config.Load(path)
	return err == nil
}

func (s *Server) BaseContext() context.Context {
	if s.baseCtx != nil {
		return s.baseCtx
	}
	return context.Background()
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return "<1s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
