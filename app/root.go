package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/internal/history"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/spf13/cobra"
)

var (
	configFile string
	groups     []string
	timeout    time.Duration
	infraOnly  bool
	logLines   int
	follow     bool
	cliHistID  string
	cliHistAt  time.Time
	cliHistCmd string
)

// extensions holds the feature sets injected by Main — consumed by the
// daemon command (server construction) and the offline doctor.
var extensions []extension.Extension

// distribution holds the binary's distribution endpoints (first non-nil
// extension.Distribution). Zero-valued in an unbranded build — env sync
// and update then require explicit configuration.
var distribution extension.Distribution

// dashboardFS holds the dashboard assets injected by Main (rooted at
// the dist contents); nil when the build embeds none.
var dashboardFS fs.FS

// Main assembles and runs the orbit CLI. version/buildTime come from the
// caller's -ldflags -X main.version/-X main.buildTime (each binary's main
// package owns the ldflags target); ui is the built dashboard the daemon
// serves (nil for a dashboard-less build); exts are the feature sets
// wired in.
func Main(versionLD, buildTimeLD string, ui fs.FS, exts []extension.Extension) {
	version, buildTime = versionLD, buildTimeLD
	dashboardFS = ui
	extensions = exts
	distribution = extension.Distribution{} // reset: a prior Main call must not leak its branding
	for _, ext := range exts {
		if ext.Distribution != nil {
			distribution = *ext.Distribution
			break
		}
	}
	rootCmd := &cobra.Command{
		Use:   "orbit",
		Short: "Local development orchestration tool",
		Long: `Run host services and containers as one local development environment.

In an existing project, add orbit.yaml to the project root and run orbit up.
Orbit discovers the nearest orbit.yaml automatically; orbit init is only needed
for the bundled demo or a shared environment repository.`,
		Version: buildVersion(),
	}
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "config file path (overrides project config and current env)")
	rootCmd.PersistentFlags().BoolVar(&cli.JSONOutput, "json", false, "output in JSON format")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Apply saved settings (workspace root, SQL mode) to env for all commands
		s := daemon.LoadSettings(daemon.DefaultSettingsPath())
		s.ApplyToEnv()

		if configFile == "" {
			configFile = resolveConfigFile()
		}
		selection := readEnvironmentSelection()
		if commandRequiresAvailableEnvironment(cmd) {
			if setupRequired(selection, configFile) {
				return setupRequiredError{}
			}
			if environmentSelectionBlocksConfig(selection, configFile) {
				return newEnvironmentSelectionRequiredError(selection)
			}
		}
		requiresMatch := commandRequiresMatchingDaemonConfig(cmd)
		requiresReconcile := commandRequiresReconciledDaemon(cmd)
		if requiresMatch || requiresReconcile {
			client := daemon.NewClient(daemon.DefaultSocketPath())
			if client.Health() == nil {
				if status, err := client.Status(); err == nil {
					if requiresMatch {
						explicitConfig := cmd.Root().PersistentFlags().Changed("config")
						if shouldResumeDetachedProject(explicitConfig, configFile, status.ConfigPath) {
							configFile = status.ConfigPath
						}
						if mismatch := daemon.CheckConfigMatch(configFile, status.ConfigPath); mismatch != nil {
							if usesDiscoveredProjectConfig(configFile) {
								return projectContextInactive(configFile, status.ConfigPath)
							}
							return mismatch
						}
					}
					if requiresReconcile {
						if stale := daemon.CheckEnvironmentReconciled(status); stale != nil {
							return stale
						}
						if version, versionErr := currentDaemonVersion(client); versionErr == nil {
							if update := checkCurrentDaemonVersion(version); update != nil {
								return update
							}
						}
					}
				}
			}
		}
		if shouldRecordCLI(cmd) {
			cliHistID = history.NewID()
			cliHistAt = time.Now()
			cliHistCmd = commandString()
			postCLIHistoryEvent(history.Record{
				ID:        cliHistID,
				Timestamp: cliHistAt,
				Source:    history.SourceCLI,
				Command:   cliHistCmd,
				Summary:   cliHistCmd,
				HasCLI:    true,
				Status:    history.StatusPending,
			})
		}
		return nil
	}

	rootCmd.AddCommand(upCmd())
	rootCmd.AddCommand(downCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(inspectCmd())
	rootCmd.AddCommand(restartCmd())
	rootCmd.AddCommand(logsCmd())
	rootCmd.AddCommand(openCmd())
	rootCmd.AddCommand(execCmd())
	rootCmd.AddCommand(queryCmd())
	rootCmd.AddCommand(topicsCmd())
	rootCmd.AddCommand(seedCmd())
	rootCmd.AddCommand(doctorCmd())
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(envCmd())
	for _, ext := range extensions {
		if ext.Commands != nil {
			rootCmd.AddCommand(ext.Commands()...)
		}
	}
	rootCmd.AddCommand(switchCmd())
	rootCmd.AddCommand(daemonCmd())
	rootCmd.AddCommand(selfUpdateCmd())
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(uninstallCmd())
	rootCmd.AddCommand(historyCmd())
	rootCmd.AddCommand(edgeCmd())
	rootCmd.AddCommand(serviceCmd())
	rootCmd.AddCommand(settingsCmd())
	rootCmd.AddCommand(traceCmd())
	rootCmd.AddCommand(tracingCmd())
	configureContextualRootHelp(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		err = contextualizeUnknownFlag(rootCmd, err)
		err = contextualizeDaemonUnavailable(err)
		printExecutionError(os.Stderr, err)
		finalizeCLIHistory(err)
		os.Exit(1)
	}
	finalizeCLIHistory(nil)
}

// contextualizeDaemonUnavailable keeps every daemon-backed command on the
// same recovery path. Before setup the user must initialize; with a selected
// project or environment, starting it is the next step. The underlying socket
// error is an implementation detail on both paths.
func contextualizeDaemonUnavailable(err error) error {
	if !errors.Is(err, daemon.ErrDaemonUnreachable) {
		return err
	}
	if setupRequired(readEnvironmentSelection(), configFile) {
		return setupRequiredError{}
	}
	return cli.NewOrbitNotRunningError()
}

func configureContextualRootHelp(root *cobra.Command) {
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == root {
			settings := daemon.LoadSettings(daemon.DefaultSettingsPath())
			settings.ApplyToEnv()
			path := configFile
			if path == "" {
				path = resolveConfigFile()
			}
			cfg, _ := config.Load(path)
			applyContextualCommandVisibility(root, cfg, extensions)
		}
		defaultHelp(cmd, args)
	})
}

func applyContextualCommandVisibility(root *cobra.Command, cfg *config.Config, exts []extension.Extension) {
	visibility := coreCommandVisibility(cfg)
	for _, ext := range exts {
		if ext.CommandVisibility == nil {
			continue
		}
		for name, visible := range ext.CommandVisibility(cfg) {
			visibility[name] = visible
		}
	}
	for name, visible := range visibility {
		if cmd, _, err := root.Find([]string{name}); err == nil && cmd != root {
			cmd.Hidden = !visible
		}
	}
}

func coreCommandVisibility(cfg *config.Config) map[string]bool {
	visibility := map[string]bool{
		"exec":   false,
		"query":  false,
		"seed":   false,
		"topics": false,
		"trace":  false,
	}
	if cfg == nil {
		return visibility
	}

	visibility["exec"] = len(cfg.Containers) > 0
	visibility["query"] = findContainer(cfg, "mongo") != "" ||
		findContainer(cfg, "redis") != "" ||
		findPostgresContainer(cfg) != ""
	if _, err := findKafkaContainer(cfg); err == nil {
		visibility["topics"] = true
	}
	for _, container := range cfg.Containers {
		if container.Seed != nil && len(container.Seed.Files) > 0 {
			visibility["seed"] = true
			break
		}
	}
	visibility["trace"] = cfg.TracingEnabled()
	return visibility
}

// printExecutionError renders a CLI failure for the user. In --json mode it
// deliberately writes the envelope to stdout (not stderr) so a single stdout
// read on the agent side yields the structured result. Mixed stdout/stderr
// would force agents to interleave readers and lose ordering — the contract
// trades the unix convention for a simpler parser on the consumer side.
func printExecutionError(w io.Writer, err error) {
	if err == nil {
		return
	}
	var rendered errCLIJSONAlreadyRendered
	if errors.As(err, &rendered) {
		return
	}
	var extensionRendered interface{ CLIJSONAlreadyRendered() }
	if errors.As(err, &extensionRendered) {
		return
	}
	if cli.JSONOutput {
		out := w
		if w == os.Stderr {
			out = os.Stdout
		}
		_ = cli.WriteJSONError(out, commandString(), err)
		return
	}
	var portConflict *cli.ResourcePortConflictError
	if errors.As(err, &portConflict) {
		_, _ = fmt.Fprintf(w, "Error: %v\n", portConflict)
		if portConflict.InspectCommand != "" {
			_, _ = fmt.Fprintf(w, "  → Inspect owner: %s\n", portConflict.InspectCommand)
		}
		_, _ = fmt.Fprintf(w, "  → Stop that process or change %s's host port, then run: orbit up\n", portConflict.Resource)
		return
	}
	_, _ = fmt.Fprintf(w, "Error: %v\n", err)
	var withContext interface{ CLIHumanContext() string }
	if errors.As(err, &withContext) && withContext.CLIHumanContext() != "" {
		_, _ = fmt.Fprintf(w, "  %s\n", withContext.CLIHumanContext())
	}
	var withHumanNext interface{ CLIHumanNextCommand() string }
	if errors.As(err, &withHumanNext) && withHumanNext.CLIHumanNextCommand() != "" {
		_, _ = fmt.Fprintf(w, "  Next: %s\n", withHumanNext.CLIHumanNextCommand())
		return
	}
	if errors.Is(err, cli.ErrLogsUnavailable) {
		var withActions interface{ CLIJSONActions() []cli.JSONAction }
		if errors.As(err, &withActions) {
			actions := withActions.CLIJSONActions()
			if len(actions) > 0 {
				next := strings.TrimSuffix(actions[0].Command, " --json")
				_, _ = fmt.Fprintf(w, "  Next: %s\n", next)
			}
		}
	}
}

type errCLIJSONAlreadyRendered struct {
	err error
}

func (e errCLIJSONAlreadyRendered) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e errCLIJSONAlreadyRendered) Unwrap() error {
	return e.err
}

func commandRequiresMatchingDaemonConfig(cmd *cobra.Command) bool {
	top := cmd
	for top.Parent() != nil && top.Parent().Parent() != nil {
		top = top.Parent()
	}
	switch top.Name() {
	case "down", "restart", "logs", "open", "exec", "query", "topics", "seed",
		"edge", "service", "sqlserver", "tunnel", "trace", "tracing":
		return true
	default:
		return false
	}
}

func commandRequiresReconciledDaemon(cmd *cobra.Command) bool {
	top := cmd
	for top.Parent() != nil && top.Parent().Parent() != nil {
		top = top.Parent()
	}
	switch top.Name() {
	case "restart", "exec", "query", "topics", "seed", "edge", "service", "sqlserver", "tunnel":
		return true
	case "env":
		return cmd.Name() == "toggle"
	case "daemon":
		return cmd.Name() == "start"
	default:
		return false
	}
}

func commandRequiresAvailableEnvironment(cmd *cobra.Command) bool {
	top := cmd
	for top.Parent() != nil && top.Parent().Parent() != nil {
		top = top.Parent()
	}
	switch top.Name() {
	case "init", "switch", "status", "inspect", "doctor", "down", "logs", "open",
		"version", "update", "uninstall", "history", "settings", "trace", "tracing":
		return false
	case "env":
		return cmd.Name() == "toggle" || cmd.Name() == "apply"
	case "daemon":
		switch cmd.Name() {
		case "stop", "status":
			return false
		default:
			return true
		}
	default:
		return true
	}
}

func shouldRecordCLI(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "history" || c.Name() == "uninstall" {
			return false
		}
	}
	if cmd.Parent() != nil && cmd.Parent().Name() == "daemon" {
		switch cmd.Name() {
		case "run", "status":
			return false
		}
	}
	return cmd.Runnable()
}

func finalizeCLIHistory(execErr error) {
	if cliHistID == "" {
		return
	}
	status := history.StatusOK
	errMsg := ""
	if execErr != nil {
		status = history.StatusError
		errMsg = execErr.Error()
	}
	postCLIHistoryEvent(history.Record{
		ID:         cliHistID,
		Timestamp:  time.Now(),
		Source:     history.SourceCLI,
		Command:    cliHistCmd,
		Summary:    cliHistCmd,
		HasCLI:     true,
		Status:     status,
		DurationMs: time.Since(cliHistAt).Milliseconds(),
		Error:      errMsg,
	})
}

func commandString() string {
	parts := make([]string, 0, len(os.Args))
	parts = append(parts, "orbit")
	if len(os.Args) > 1 {
		parts = append(parts, os.Args[1:]...)
	}
	for i := 1; i < len(parts); i++ {
		parts[i] = shellquote.Quote(parts[i])
	}
	return strings.Join(parts, " ")
}

// version and buildTime are injected via -ldflags at build time. Version is
// the short commit hash (with optional "-dirty" suffix); buildTime is the
// RFC3339 UTC timestamp of when the binary was built. Both fall back to VCS
// stamps embedded by the Go toolchain when empty.
var (
	version   string
	buildTime string
)

// buildVersion returns "<rev>[-dirty] (<time>)" — e.g.
// "98c7e7a12345 (2026-04-18 20:03:47 +0800)". When the `version` ldflag is a
// release tag (starts with "v" or contains "."), it's used verbatim in place
// of the truncated SHA.
func buildVersion() string {
	rev, t := version, buildTime
	tagged := isReleaseTag(rev)
	if rev == "" {
		rev, t = vcsVersion(t)
	}
	if rev == "" {
		return "unknown"
	}
	if !tagged {
		rev = truncateSHA(rev)
	}
	if d := formatTime(t); d != "" {
		return fmt.Sprintf("%s (%s)", rev, d)
	}
	return rev
}

// isReleaseTag returns true for ldflag-injected release versions like "v1.2.3"
// or "1.2.3" — anything clearly not a commit SHA.
func isReleaseTag(v string) bool {
	if v == "" {
		return false
	}
	return strings.HasPrefix(v, "v") || strings.Contains(v, ".")
}

// truncateSHA shortens a commit SHA to 12 chars, preserving a "-dirty" suffix.
func truncateSHA(rev string) string {
	dirty := ""
	if strings.HasSuffix(rev, "-dirty") {
		dirty = "-dirty"
		rev = strings.TrimSuffix(rev, "-dirty")
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	return rev + dirty
}

// formatTime parses an RFC3339 timestamp and returns it formatted in local
// time as "2006-01-02 15:04:05 -0700". Falls back to the original string if
// parsing fails.
func formatTime(t string) string {
	if t == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, t); err == nil {
		return parsed.Local().Format("2006-01-02 15:04:05 -0700")
	}
	return t
}

// vcsVersion reads the Go toolchain's embedded VCS stamps. Returns the short
// revision (with "-dirty" suffix if the working tree was modified) and, when
// no build-time override was supplied, the commit timestamp as a fallback.
func vcsVersion(existingTime string) (string, string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", existingTime
	}
	var rev, commitTime string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			commitTime = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "", existingTime
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	if existingTime == "" {
		existingTime = commitTime
	}
	return rev, existingTime
}

// ============================================================
// Command definitions
// ============================================================

func upCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up [resources...]",
		Short: "Start resources (or all if no args)",
		Long: `Start containers and host services. Orbit starts everything it needs automatically.

Examples:
  orbit up                    # start everything (containers + services)
  orbit up --infra            # start only containers
  orbit up api redis          # start specific resources
  orbit up --group frontend   # start one configured group

Resource names, --infra, and --group are separate selection modes and cannot be combined.`,
		RunE: runUp,
	}
	cmd.Flags().StringSliceVar(&groups, "group", nil, "enable specific groups (comma-separated)")
	cmd.Flags().BoolVar(&infraOnly, "infra", false, "start only infrastructure containers")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "exit after duration (e.g. 60s)")
	return cmd
}

func downCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down [resources...]",
		Short: "Stop resources (or all if no selection is given)",
		Long: `Stop containers and host services using the same selection modes as orbit up.

Examples:
  orbit down                    # stop everything
  orbit down api redis          # stop specific resources
  orbit down --group frontend   # stop one configured group
  orbit down --infra            # stop only containers

Resource names, --infra, and --group are separate selection modes and cannot be combined.
Orbit remains ready for the next 'orbit up'.`,
		RunE: runDown,
	}
	cmd.Flags().StringSliceVar(&groups, "group", nil, "stop specific groups (comma-separated)")
	cmd.Flags().BoolVar(&infraOnly, "infra", false, "stop only infrastructure containers")
	return cmd
}

func restartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart <resource>",
		Short: "Restart a resource",
		Args:  cobra.ExactArgs(1),
		RunE:  runRestart,
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "exit after duration (e.g. 60s)")
	return cmd
}

func logsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs <resource>",
		Short: "Show or stream resource logs",
		Args:  cobra.ExactArgs(1),
		RunE:  runLogs,
	}
	cmd.Flags().IntVar(&logLines, "lines", 100, "number of log lines to show")
	cmd.Flags().IntVar(&logLines, "tail", 100, "number of log lines to show (alias for --lines)")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream logs in real-time")
	return cmd
}

func openCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open [service]",
		Short: "Open the dashboard or a service in the browser",
		Long:  "Open the Orbit dashboard. Pass a service name to open that service instead.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runOpen,
	}
}

func daemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Manage the orbit daemon",
		Hidden: true,
	}

	runCmd := &cobra.Command{
		Use:    "run",
		Short:  "Run the daemon in foreground (internal)",
		Hidden: true,
		RunE:   runDaemon,
	}
	runCmd.Flags().StringSliceVar(&groups, "group", nil, "enable specific groups")

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon in background",
		Long:  "Start the orbit supervisor process. Does not start containers or services — use 'orbit up' for that.",
		RunE:  runDaemonStart,
	}
	startCmd.Flags().StringSliceVar(&groups, "group", nil, "enable specific groups")

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon",
		Long:  "Stop the orbit supervisor process and dev services. Containers are left running — use 'orbit down' first to stop them.",
		RunE:  runDaemonStop,
	}

	restartCmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the daemon (e.g. after upgrading the binary)",
		RunE:  runDaemonRestart,
	}
	restartCmd.Flags().StringSliceVar(&groups, "group", nil, "enable specific groups")
	restartCmd.Flags().DurationVar(&daemonRestartDelay, "handoff-delay", 0, "delay restart for dashboard handoff")
	_ = restartCmd.Flags().MarkHidden("handoff-delay")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status, version, and config",
		RunE:  runDaemonStatus,
	}

	cmd.AddCommand(runCmd, startCmd, stopCmd, restartCmd, statusCmd)
	return cmd
}

// ============================================================
// Command implementations
// ============================================================

func runDown(_ *cobra.Command, args []string) error {
	if err := validateLifecycleSelection(args, groups, infraOnly); err != nil {
		return err
	}
	if err := validateConfiguredLifecycleSelection(configFile, args, groups, "down"); err != nil {
		return err
	}
	client := daemon.NewClient(daemon.DefaultSocketPath())
	// After an abrupt daemon exit its containers and processes survive. Claiming
	// "already stopped" would leak them while reporting success, so adopt the
	// orphans through a fresh daemon and stop them for real.
	adoptedOrphans := false
	if err := client.Health(); err != nil {
		orphaned, orphanErr := reconcilableResourcesExist(configFile)
		if orphanErr != nil || !orphaned {
			message := downAlreadyStoppedMessage
			actions := lifecycleDownSuccessActions()
			if setupRequired(readEnvironmentSelection(), configFile) {
				message = downBeforeSetupMessage
				actions = lifecycleSetupActions()
			}
			return writeDownShortCircuit(message, actions, nil)
		}
		reconciled, ensureErr := daemon.EnsureDaemon(configFile, nil)
		if ensureErr != nil {
			return renderDaemonStartError(ensureErr)
		}
		client = reconciled
		adoptedOrphans = true
	}
	// Snapshot the full service+container list BEFORE issuing Down —
	// containers may be removed by the time we poll, leaving us with no
	// list to track progress against.
	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	if len(args) > 0 || len(groups) > 0 || infraOnly {
		return runStopSelection(client, status, args)
	}
	names := make([]string, 0, len(status.Resources))
	for i := range status.Resources {
		names = append(names, status.Resources[i].Name)
	}

	// A repeat 'down' would otherwise replay every resource as freshly stopped
	// work. Report the no-op the same way a selective 'down' already does.
	// A daemon started to adopt orphans has not polled yet, so its snapshot
	// still reads "stopped" — persisted state already proved there is work.
	if !adoptedOrphans && !anyResourceActive(status) {
		return writeDownShortCircuit(downAlreadyStoppedMessage, lifecycleDownSuccessActions(), status)
	}

	if cli.JSONOutput {
		_, err := client.Down(false)
		if err != nil {
			return fmt.Errorf("down failed: %w", err)
		}
		finalStatus, err := waitForLifecycleJSON(client, names, "stopped")
		if err != nil {
			return err
		}
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildLifecycleJSONData(lifecycleJSONOptions{
			Operation:          "down",
			Message:            downCompletionMessage,
			RequestedResources: names,
			FinalStatus:        finalStatus,
		}), lifecycleDownSuccessActions())
	}

	if _, err := client.Down(false); err != nil {
		return fmt.Errorf("down failed: %w", err)
	}
	if err := waitForServicesStopped(client, names, false); err != nil {
		return err
	}
	fmt.Println(downCompletionMessage)
	return nil
}

func runStopSelection(client *daemon.Client, status *daemon.StatusResponse, names []string) error {
	for index, name := range names {
		if lifecycleResourceExists(status, name) {
			continue
		}
		return newResourceNameError(status, name, func(suggestion string) string {
			corrected := append([]string{}, names...)
			corrected[index] = suggestion
			return "orbit down " + strings.Join(corrected, " ")
		})
	}
	resp, err := client.StopSelection(daemon.DownRequest{
		Resources: names,
		Groups:    groups,
		InfraOnly: infraOnly,
	})
	if err != nil {
		return fmt.Errorf("down failed: %w", err)
	}
	affected := resp.AffectedResources
	if len(affected) == 0 {
		if cli.JSONOutput {
			return cli.WriteJSONSuccess(os.Stdout, commandString(), buildLifecycleJSONData(lifecycleJSONOptions{
				Operation: "down",
				Message:   resp.Message,
				InfraOnly: infraOnly,
			}), nil)
		}
		fmt.Println(resp.Message)
		return nil
	}
	if cli.JSONOutput {
		finalStatus, err := waitForLifecycleJSON(client, affected, "stopped")
		if err != nil {
			return cli.WithJSONActions(err, lifecycleRecommendedActions(affected))
		}
		impacted := lifecycleDownImpacts(finalStatus, affected)
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildLifecycleJSONData(lifecycleJSONOptions{
			Operation:          "down",
			Message:            lifecycleDownResultMessage(resp.Message, impacted),
			RequestedResources: affected,
			RelatedResources:   impacted,
			InfraOnly:          infraOnly,
			FinalStatus:        finalStatus,
		}), lifecycleDownImpactActions(finalStatus, impacted))
	}
	fmt.Println(resp.Message)
	if err := waitForServicesStopped(client, affected, false); err != nil {
		return err
	}
	finalStatus, err := client.Status()
	if err != nil {
		return fmt.Errorf("status after down: %w", err)
	}
	printLifecycleDownImpacts(finalStatus, lifecycleDownImpacts(finalStatus, affected))
	return nil
}

func runRestart(_ *cobra.Command, args []string) error {
	if err := validateConfiguredCommandResources(configFile, args, func(_ int, suggestion string) string {
		return "orbit restart " + suggestion
	}); err != nil {
		return err
	}
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return cli.NewOrbitNotRunningError()
	}

	name := args[0]
	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	if !lifecycleResourceExists(status, name) {
		return newResourceNameError(status, name, func(suggestion string) string {
			return "orbit restart " + suggestion
		})
	}
	if cli.JSONOutput {
		priorRestartCount := lifecycleRestartCount(status, name)
		resp, err := client.Restart(name)
		if err != nil {
			return fmt.Errorf("restart failed: %w", err)
		}
		if observedStatus, err := waitForLifecycleRestartObserved(client, name, priorRestartCount); err != nil {
			return cli.WithJSONReplacementActions(err, lifecycleRecommendedActionsForStatus([]string{name}, observedStatus))
		}
		if stoppedStatus, err := waitForLifecycleJSONOrPast(client, []string{name}, "stopped", lifecycleRestartPastStopState); err != nil {
			return cli.WithJSONReplacementActions(err, lifecycleRecommendedActionsForStatus([]string{name}, stoppedStatus))
		}
		finalStatus, err := waitForLifecycleRestartHealthyJSON(client, []string{name})
		if err != nil {
			return cli.WithJSONReplacementActions(err, lifecycleRecommendedActionsForStatus([]string{name}, finalStatus))
		}
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildLifecycleJSONData(lifecycleJSONOptions{
			Operation:          "restart",
			Message:            resp.Message,
			RequestedResources: []string{name},
			FinalStatus:        finalStatus,
		}), nil)
	}

	if _, err := client.Restart(name); err != nil {
		return fmt.Errorf("restart failed: %w", err)
	}
	if err := waitForServicesStopped(client, []string{name}, true); err != nil {
		return err
	}
	return waitForServicesHealthy(client, []string{name})
}

func runLogs(_ *cobra.Command, args []string) error {
	if err := validateConfiguredCommandResources(configFile, args, func(_ int, suggestion string) string {
		command := "orbit logs " + suggestion
		if follow {
			command += " --follow"
		}
		return command
	}); err != nil {
		return err
	}
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return cli.NewOrbitNotRunningError()
	}

	name := args[0]
	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	resource := lifecycleResourceStatus(status, name)
	if resource == nil {
		return newResourceNameError(status, name, func(suggestion string) string {
			command := "orbit logs " + suggestion
			if follow {
				command += " --follow"
			}
			return command
		})
	}

	if follow {
		if cli.JSONOutput {
			if err := client.StreamLogs(name, func(line string) {
				_ = writeLogJSONEvent(os.Stdout, name, line)
			}); err != nil {
				if writeErr := writeLogJSONErrorEvent(os.Stdout, name, err); writeErr != nil {
					_, _ = fmt.Fprintf(os.Stderr, "failed writing log stream error event: %v\n", writeErr)
				}
				return errCLIJSONAlreadyRendered{err: err}
			}
			return nil
		}
		fmt.Printf("Streaming logs for %s (Ctrl+C to stop)...\n", name)
		return client.StreamLogs(name, func(line string) {
			fmt.Println(line)
		})
	}

	resp, err := client.Logs(name, logLines)
	if err != nil {
		return fmt.Errorf("logs failed: %w", err)
	}
	if (resp == nil || len(resp.Lines) == 0) && !resource.LogsAvailable &&
		(resource.State == "degraded" || resource.State == "pending" || resource.State == "stopped") {
		err := cli.NewLogsUnavailableError("no logs for " + name + ": the resource did not start")
		return cli.WithJSONActions(err, noLogsRecoveryActions(resource))
	}
	if latest, statusErr := client.Status(); statusErr == nil {
		if current := lifecycleResourceStatus(latest, name); current != nil {
			resource = current
		}
	}
	actions := logsRecoveryActions(resource, projectDependencySetupCommand(name))
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), buildLogsJSONData(name, logLines, resp), actions)
	}
	for _, line := range resp.Lines {
		fmt.Println(line)
	}
	if resource.FailureKind == string(engine.FailureKindHealth) {
		fmt.Printf("Health check is still failing: %s\n", resource.StateReason)
		fmt.Println("Orbit will recover automatically when the endpoint is healthy; restarting only retries the same startup.")
	}
	if len(actions) == 1 {
		fmt.Println("Next: " + strings.TrimSuffix(actions[0].Command, " --json"))
	}
	return nil
}

func projectDependencySetupCommand(resource string) string {
	cfg, err := config.Load(configFile)
	if err != nil {
		return ""
	}
	command, _ := daemonsrv.ProjectDependencySetupCommand(cfg, resource)
	return command
}

type logsJSONData struct {
	Resource       string   `json:"resource"`
	LinesRequested int      `json:"lines_requested"`
	Lines          []string `json:"lines"`
	Truncated      bool     `json:"truncated"`
}

type logJSONEvent struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	Resource      string `json:"resource"`
	Line          string `json:"line"`
}

type logJSONErrorEvent struct {
	SchemaVersion string        `json:"schema_version"`
	Type          string        `json:"type"`
	Resource      string        `json:"resource"`
	Error         cli.JSONError `json:"error"`
}

func buildLogsJSONData(resource string, requested int, resp *daemon.LogsResponse) logsJSONData {
	lines := []string{}
	if resp != nil && resp.Lines != nil {
		lines = resp.Lines
	}
	return logsJSONData{
		Resource:       resource,
		LinesRequested: requested,
		Lines:          lines,
		Truncated:      requested > 0 && len(lines) >= requested,
	}
}

func writeLogJSONEvent(w io.Writer, resource, line string) error {
	return json.NewEncoder(w).Encode(logJSONEvent{
		SchemaVersion: cli.SchemaVersion,
		Type:          "log",
		Resource:      resource,
		Line:          line,
	})
}

func writeLogJSONErrorEvent(w io.Writer, resource string, err error) error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return json.NewEncoder(w).Encode(logJSONErrorEvent{
		SchemaVersion: cli.SchemaVersion,
		Type:          "error",
		Resource:      resource,
		Error: cli.JSONError{
			Code:      "log_stream_error",
			Message:   msg,
			Hint:      "Retry the stream or inspect daemon state with 'orbit status --json'.",
			Retryable: true,
		},
	})
}

func runOpen(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return runDashboard()
	}
	if err := validateConfiguredCommandResources(configFile, args, func(_ int, suggestion string) string {
		return "orbit open " + suggestion
	}); err != nil {
		return err
	}

	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return cli.NewOrbitNotRunningError()
	}

	name := args[0]
	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("status failed: %w", err)
	}

	for i := range status.Resources {
		svc := &status.Resources[i]
		if svc.Name == name {
			if svc.URL == "" {
				return resourceURLNotConfiguredError{name: name}
			}
			if svc.State != "healthy" {
				command := "orbit status"
				reason := "Inspect the current state before opening " + name + "."
				switch svc.State {
				case "stopped", "pending":
					command = "orbit up " + name
					reason = "Start " + name + " before opening it."
				case "degraded":
					command = "orbit logs " + name
					reason = "Review " + name + "'s failure evidence before retrying it."
				}
				return cli.WithJSONReplacementActions(
					fmt.Errorf("%s is %s, not healthy — run '%s'", name, svc.State, command),
					[]cli.JSONAction{{
						Command:     command + " --json",
						Reason:      reason,
						Destructive: false,
					}},
				)
			}
			return openURL(svc.URL, "service", name)
		}
	}
	return newResourceNameError(status, name, func(suggestion string) string {
		return "orbit open " + suggestion
	})
}

type resourceURLNotConfiguredError struct {
	name string
}

func (e resourceURLNotConfiguredError) Error() string {
	return e.name + " does not expose an application URL"
}

func (e resourceURLNotConfiguredError) Unwrap() error {
	return cli.ErrNotConfigured
}

func (e resourceURLNotConfiguredError) CLIJSONHint() string {
	return "Open the dashboard to inspect this resource. Environment authors can add 'url' when it represents an application."
}

func (e resourceURLNotConfiguredError) CLIJSONReplacementActions() []cli.JSONAction {
	return []cli.JSONAction{{
		Command:     "orbit open --json",
		Reason:      "Open the dashboard to inspect " + e.name + ".",
		Destructive: false,
	}}
}

func (e resourceURLNotConfiguredError) CLIHumanNextCommand() string {
	return "orbit open"
}
