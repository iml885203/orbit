package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/preflight"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

func printLogo() {
	if !isTerminal() {
		return
	}

	stars := cli.Faint.Sprint
	frame := cli.Faint.Sprint
	block := cli.Bold.Sprint

	fmt.Println(stars("                  .  ·  *  .  ·"))
	fmt.Println(stars("             .                    ."))
	fmt.Println(frame("  ╭───────────────────────────────────────╮"))
	fmt.Println(frame("  │ ") + block(" ██████╗ ██████╗ ██████╗ ██╗████████╗") + frame(" │"))
	fmt.Println(frame("  │ ") + block("██╔═══██╗██╔══██╗██╔══██╗██║╚══██╔══╝") + frame(" │"))
	fmt.Println(frame("  │ ") + block("██║   ██║██████╔╝██████╔╝██║   ██║   ") + frame(" │"))
	fmt.Println(frame("  │ ") + block("██║   ██║██╔══██╗██╔══██╗██║   ██║   ") + frame(" │"))
	fmt.Println(frame("  │ ") + block("╚██████╔╝██║  ██║██████╔╝██║   ██║   ") + frame(" │"))
	fmt.Println(frame("  │ ") + block(" ╚═════╝ ╚═╝  ╚═╝╚═════╝ ╚═╝   ╚═╝   ") + frame(" │"))
	fmt.Println(frame("  ╰───────────────────────────────────────╯"))
	fmt.Println(stars("             *                    ·"))
	fmt.Println(stars("                  ·  .  *  ·"))
	_, _ = cli.Faint.Println("        local dev orchestrator")
	fmt.Println()
}

func isTerminal() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

func runUp(cmd *cobra.Command, args []string) error {
	if err := validateUpSelection(args); err != nil {
		return err
	}
	if err := validateConfiguredLifecycleSelection(configFile, args, groups, "up"); err != nil {
		return err
	}
	configWithoutManagedEnvironment := cmd.Root().PersistentFlags().Changed("config") ||
		usesDiscoveredProjectConfig(configFile)
	if err := preflightOrAbort(configWithoutManagedEnvironment, args); err != nil {
		return err
	}
	contextSwitch, err := switchDiscoveredProjectContext()
	if err != nil {
		if errors.Is(err, errProjectContextSwitchDeclined) {
			fmt.Println("Aborted.")
			return nil
		}
		return err
	}
	printProjectEnvironmentContext()

	if cli.JSONOutput {
		return runUpJSON(args, contextSwitch)
	}

	client, err := daemon.EnsureDaemonWithContext(configFile, groups, environmentContextKind(configFile))
	if err != nil {
		return renderDaemonStartError(err)
	}
	client, appliedChanges, err := convergeEnvironmentChangesForUp(
		client,
		environmentApplyProgress(),
	)
	if err != nil {
		return err
	}
	if err := validateUpResourceNames(client, args); err != nil {
		return err
	}

	req := daemon.UpRequest{
		Resources: args,
		InfraOnly: infraOnly,
		Groups:    groups,
	}

	client, resp, appliedChanges, err := startWithEnvironmentConvergence(
		client,
		req,
		appliedChanges,
		environmentApplyProgress(),
	)
	if err != nil {
		return fmt.Errorf("up failed: %w", err)
	}
	fmt.Println(resp.Message)

	if len(resp.AffectedResources) == 0 {
		printUpSuccessNextStep(nil, nil)
		return nil
	}
	return waitForUpHealthy(client, resp.AffectedResources, upCompletionMessage(args))
}

func upCompletionMessage(args []string) string {
	switch {
	case infraOnly:
		return "Infrastructure is healthy."
	case len(args) == 1:
		return args[0] + " is healthy."
	case len(args) > 1:
		return "Requested resources are healthy."
	case len(groups) == 1:
		return "Group " + groups[0] + " is healthy."
	case len(groups) > 1:
		return "Selected groups are healthy."
	default:
		return "Environment is healthy."
	}
}

func validateUpSelection(args []string) error {
	return validateLifecycleSelection(args, groups, infraOnly)
}

func runUpJSON(args []string, contextSwitch *projectContextSwitch) error {
	client, err := daemon.EnsureDaemonWithContext(configFile, groups, environmentContextKind(configFile))
	if err != nil {
		return renderDaemonStartError(err)
	}
	client, appliedChanges, err := convergeEnvironmentChangesForUp(client, nil)
	if err != nil {
		return err
	}
	if err := validateUpResourceNames(client, args); err != nil {
		return err
	}
	req := daemon.UpRequest{Resources: args, InfraOnly: infraOnly, Groups: groups}
	client, resp, appliedChanges, err := startWithEnvironmentConvergence(
		client,
		req,
		appliedChanges,
		nil,
	)
	if err != nil {
		return fmt.Errorf("up failed: %w", err)
	}
	names := resp.AffectedResources
	finalStatus, err := waitForLifecycleJSON(client, names, "healthy")
	if err != nil {
		failure := cli.WithJSONReplacementActions(err, lifecycleRecommendedActionsForStatus(names, finalStatus))
		data := buildUpFailureJSONData(names, finalStatus, func(name string) []string {
			return recentLogTail(client, name)
		})
		if writeErr := cli.WriteJSONFailure(os.Stdout, commandString(), data, failure, nil); writeErr != nil {
			return writeErr
		}
		return errCLIJSONAlreadyRendered{err: failure}
	}
	return cli.WriteJSONSuccess(os.Stdout, commandString(), buildLifecycleJSONData(lifecycleJSONOptions{
		Operation:          "up",
		Message:            resp.Message,
		RequestedResources: names,
		InfraOnly:          infraOnly,
		FinalStatus:        finalStatus,
		ContextSwitch:      contextSwitch,
		EnvironmentChanges: lifecycleEnvironmentChanges(appliedChanges),
	}), lifecycleUpSuccessActions(names, finalStatus))
}

func convergeEnvironmentChangesForUp(
	client *daemon.Client,
	report func(string),
) (*daemon.Client, environmentApplyResult, error) {
	result, err := applyEnvironmentChanges(report)
	if err != nil {
		return client, result, err
	}
	if result.Applied {
		client = daemon.NewClient(daemon.DefaultSocketPath())
	}
	return client, result, nil
}

func startWithEnvironmentConvergence(
	client *daemon.Client,
	request daemon.UpRequest,
	applied environmentApplyResult,
	report func(string),
) (*daemon.Client, *daemon.APIResponse, environmentApplyResult, error) {
	response, err := client.Up(request)
	var stale *daemon.ConfigStaleError
	if !errors.As(err, &stale) {
		return client, response, applied, err
	}

	applied, err = applyEnvironmentChangesKnownPending(report)
	if err != nil {
		return client, nil, applied, err
	}
	client = daemon.NewClient(daemon.DefaultSocketPath())
	if err := validateUpResourceNames(client, request.Resources); err != nil {
		return client, nil, applied, err
	}
	response, err = client.Up(request)
	return client, response, applied, err
}

func validateUpResourceNames(client *daemon.Client, names []string) error {
	if len(names) == 0 {
		return nil
	}
	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("status before up: %w", err)
	}
	for index, name := range names {
		if lifecycleResourceExists(status, name) {
			continue
		}
		return newResourceNameError(status, name, func(suggestion string) string {
			corrected := append([]string{}, names...)
			corrected[index] = suggestion
			return "orbit up " + strings.Join(corrected, " ")
		})
	}
	return nil
}

// pollLoop runs an onTick callback every 2s with the latest status snapshot
// until onTick returns done=true, returns an error, the timeout fires, or
// the user sends SIGINT/SIGTERM (treated as detach, not failure).
// Caller-owned state (reported maps, snapshots, etc.) is captured via the
// onTick closure — pollLoop holds none of it.
func pollLoop(
	client *daemon.Client,
	timeoutDur time.Duration,
	waitingFor string,
	onTick func(t time.Time, status *daemon.StatusResponse) (done bool, err error),
) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	const frameInterval = 100 * time.Millisecond
	const pollInterval = 2 * time.Second
	frame := time.NewTicker(frameInterval)
	defer frame.Stop()
	budget := effectiveTimeout(timeoutDur)
	started := time.Now()
	deadline := time.After(budget)

	var lastPoll time.Time
	var status *daemon.StatusResponse

	for {
		select {
		case <-ctx.Done():
			fmt.Println()
			_, _ = cli.Faint.Println("Detached. Daemon is still running — use 'orbit status' to check progress.")
			return nil
		case <-deadline:
			return newWaitTimeoutError(waitingFor, timeoutDur, budget, time.Since(started))
		case t := <-frame.C:
			// Poll only every pollInterval; the frame ticker still fires
			// every 100ms so the caller can animate (spinner, seconds).
			if t.Sub(lastPoll) >= pollInterval || status == nil {
				if s, err := client.Status(); err == nil {
					status = s
					lastPoll = t
				}
			}
			if status == nil {
				continue
			}
			done, err := onTick(t, status)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

// waitOptions parameterises the boilerplate every waitFor* function shares:
// renderer selection, frame counter, snapshot/diff/render plumbing, and
// finalize-on-error wrapping. Callers supply only the bits that differ —
// filter, commit predicate, done predicate, optional post-done acceptance,
// optional grace polling, and a tick handler that decides whether the
// caller's terminal condition is satisfied.
type waitOptions struct {
	// filter selects which services from the daemon status to track.
	// If nil, all services are tracked.
	filter func(*daemon.ResourceStatus) bool
	// commit returns true when an event should be promoted into the
	// renderer's permanent log above the in-place region.
	commit func(progressEvent) bool
	// doneOn returns true when an event marks a service as completed.
	doneOn func(progressEvent) bool
	// pastDone, if non-nil, accepts a snapshot state as "already past
	// the awaited transition" — used by waitForServicesStopped's
	// restart-race handling. Without it, only doneOn promotes services.
	pastDone func(state string) bool
	// onTick is called after each poll. It decides whether the wait is
	// finished, by inspecting the current snapshots and done map. It may
	// also fail the wait with an error.
	onTick func(snapshots map[string]progressSnapshot, done map[string]bool, status *daemon.StatusResponse) (bool, error)
	// waitingFor names what the wait was for, e.g. "resources to become
	// healthy". It is rendered into the timeout message alongside the
	// budget, which is only known once the deadline actually fires.
	waitingFor string
}

// runProgressWait is the shared driver behind every waitFor* function. It
// owns the renderer, snapshot/done state, frame counter, and finalize
// wrapping; the caller controls filter / commit / done / terminal
// condition through waitOptions.
func runProgressWait(client *daemon.Client, opts waitOptions) error {
	var renderer progressRenderer
	if isTerminal() {
		renderer = newLiveRenderer(os.Stdout)
	} else {
		renderer = newAppendRenderer(os.Stdout)
	}

	snapshots := make(map[string]progressSnapshot)
	done := make(map[string]bool)
	frame := 0

	err := pollLoop(client, timeout, opts.waitingFor,
		func(t time.Time, status *daemon.StatusResponse) (bool, error) {
			frame++
			watched := make([]daemon.ResourceStatus, 0, len(status.Resources))
			progressByName := make(map[string]*daemon.HealthProgressInfo, len(status.Resources))
			for i := range status.Resources {
				s := &status.Resources[i]
				if opts.filter != nil && !opts.filter(s) {
					continue
				}
				watched = append(watched, *s)
				progressByName[s.Name] = s.HealthProgress
			}
			prev := snapshots
			next := nextSnapshots(prev, watched, t)
			for _, evt := range diffProgress(prev, next, t) {
				line := formatProgressEvent(evt)
				if opts.commit != nil && opts.commit(evt) {
					renderer.commit(coloredEvent(evt, line))
				}
				if opts.doneOn != nil && opts.doneOn(evt) {
					done[evt.name] = true
				}
				if evt.kind == eventHeartbeat {
					s := next[evt.name]
					s.lastHeartbeat = t
					next[evt.name] = s
				}
			}
			snapshots = next
			renderer.render(snapshots, progressByName, t, frame)

			if opts.pastDone != nil {
				for i := range watched {
					name := watched[i].Name
					if done[name] {
						continue
					}
					if opts.pastDone(watched[i].State) {
						done[name] = true
					}
				}
			}

			finished, err := opts.onTick(snapshots, done, status)
			if err != nil {
				renderer.finalize(false)
				return false, err
			}
			if finished {
				renderer.finalize(true)
				return true, nil
			}
			return false, nil
		},
	)
	if err != nil {
		renderer.finalize(false)
	}
	return err
}

// announceRecovering prints a one-time notice per service that entered
// recovery probing. Without it the wait goes silent: the degraded line is
// already committed, and the next visible change (recovery or timeout) can
// be minutes away — silence reads as a hang.
func announceRecovering(snapshots map[string]progressSnapshot, announced map[string]bool) {
	for name, s := range snapshots {
		if s.state == "degraded" && s.recovering && !announced[name] {
			announced[name] = true
			_, _ = cli.Faint.Printf(
				"  … %s degraded — retrying health checks for up to %s\n",
				name,
				effectiveTimeout(timeout),
			)
		}
	}
}

func waitForUpHealthy(client *daemon.Client, serviceNames []string, completionMessage string) error {
	watch := watchSet(serviceNames)
	announced := map[string]bool{}
	return runProgressWait(client, waitOptions{
		filter:     watchFilter(watch),
		commit:     commitOnHealthyOrDegraded,
		doneOn:     doneOnHealthy,
		waitingFor: "resources to become healthy",
		onTick: func(snapshots map[string]progressSnapshot, done map[string]bool, status *daemon.StatusResponse) (bool, error) {
			announceRecovering(snapshots, announced)
			for name := range watch {
				s, ok := snapshots[name]
				if !ok {
					continue
				}
				if err := blockedDependencyError(client, status, name, s); err != nil {
					return false, err
				}
				if s.state == "degraded" && !s.recovering {
					return false, serviceStartError(name, s, recentLogEvidence(client, name))
				}
			}
			if len(done) == len(watch) {
				fmt.Println(completionMessage)
				printUpSuccessNextStep(serviceNames, status)
				return true, nil
			}
			return false, nil
		},
	})
}

func printUpSuccessNextStep(resourceNames []string, status *daemon.StatusResponse) {
	resource := primaryOpenableResource(resourceNames, status)
	if resource == nil {
		_, _ = cli.Faint.Println("  Next: orbit open          open dashboard")
		return
	}
	fmt.Printf("  %s: %s\n", resource.Name, resource.URL)
	_, _ = cli.Faint.Printf("  Next: orbit open %s\n", resource.Name)
	_, _ = cli.Faint.Println("  Dashboard: orbit open")
}

func waitForServicesHealthy(client *daemon.Client, serviceNames []string) error {
	if len(serviceNames) == 0 {
		return nil
	}
	watch := watchSet(serviceNames)
	announced := map[string]bool{}
	return runProgressWait(client, waitOptions{
		filter:     watchFilter(watch),
		commit:     commitOnHealthyOrDegraded,
		doneOn:     doneOnHealthy,
		waitingFor: "requested resources to become healthy",
		onTick: func(snapshots map[string]progressSnapshot, done map[string]bool, status *daemon.StatusResponse) (bool, error) {
			announceRecovering(snapshots, announced)
			for name := range watch {
				if done[name] {
					continue
				}
				s, ok := snapshots[name]
				if !ok {
					continue
				}
				if err := blockedDependencyError(client, status, name, s); err != nil {
					return false, err
				}
				if s.state == "stopped" {
					return false, fmt.Errorf("%s stopped before becoming healthy", name)
				}
				if s.state == "degraded" && !s.recovering {
					return false, serviceStartError(name, s, recentLogEvidence(client, name))
				}
			}
			if len(done) == len(watch) {
				if len(serviceNames) == 1 {
					fmt.Printf("%s is healthy.\n", serviceNames[0])
				} else {
					fmt.Println("Requested resources are healthy.")
				}
				return true, nil
			}
			return false, nil
		},
	})
}

// waitForServicesStopped polls until every service in serviceNames has
// reached the "stopped" state. When acceptPastStop is true (used by
// restart), services that are already past stopping (pending/starting/
// healthy/...) also count as done — this handles the race where the
// daemon's restart goroutine moves stop→start before our first poll
// lands. For stop/down, acceptPastStop should be false: we want the
// real "stopped" transition to render.
func waitForServicesStopped(client *daemon.Client, serviceNames []string, acceptPastStop bool) error {
	if len(serviceNames) == 0 {
		return nil
	}
	watch := watchSet(serviceNames)
	// A watched service that moves stopping → degraded failed its stop
	// (StopService parks it there). Waiting for "stopped" would burn the
	// whole timeout, so count it as done-with-failure and report once the
	// remaining services settle. Restart (acceptPastStop) keeps the old
	// behavior: its stop phase re-enters pending even when the stop errors.
	var stopFailed []string
	isStopFailure := func(e progressEvent) bool {
		return !acceptPastStop && e.kind == eventTransition && e.from == "stopping" && e.to == "degraded"
	}
	opts := waitOptions{
		filter: watchFilter(watch),
		commit: func(e progressEvent) bool {
			return (e.kind == eventTransition && e.to == "stopped") || isStopFailure(e)
		},
		doneOn: func(e progressEvent) bool {
			if isStopFailure(e) {
				stopFailed = append(stopFailed, e.name)
				return true
			}
			return e.kind == eventTransition && e.to == "stopped"
		},
		waitingFor: "resources to stop",
		onTick: func(_ map[string]progressSnapshot, done map[string]bool, _ *daemon.StatusResponse) (bool, error) {
			if len(done) == len(watch) {
				if len(stopFailed) > 0 {
					return false, fmt.Errorf("stop failed for %s — check 'orbit status' and 'docker ps'", strings.Join(stopFailed, ", "))
				}
				return true, nil
			}
			return false, nil
		},
	}
	if acceptPastStop {
		opts.pastDone = isPostStopState
	}
	return runProgressWait(client, opts)
}

func blockedDependencyError(client *daemon.Client, status *daemon.StatusResponse, serviceName string, snapshot progressSnapshot) error {
	if snapshot.state != "pending" || len(snapshot.pendingDependencies) == 0 || status == nil {
		return nil
	}
	dependency := terminalDependencyBlocker(status, snapshot.pendingDependencies)
	if dependency == nil {
		return nil
	}
	if dependency.PortConflict != nil {
		return resourcePortConflictError(dependency.PortConflict)
	}
	reason := dependency.StateReason
	if reason == "" && dependency.HealthProgress != nil {
		reason = dependency.HealthProgress.LastErr
	}
	if reason == "" {
		reason = dependency.State
	}
	dependencySnapshot := progressSnapshot{reason: reason}
	evidence := ""
	if dependency.State != "stopped" {
		evidence = recentLogEvidence(client, dependency.Name)
	}
	return fmt.Errorf(
		"%s cannot start because dependency %s is unhealthy\n  %w",
		serviceName,
		dependency.Name,
		serviceStartError(dependency.Name, dependencySnapshot, evidence),
	)
}

// watchSet builds the set used by filter / done-count comparisons.
func watchSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// watchFilter returns a waitOptions.filter that keeps only services
// whose name is in watch.
func watchFilter(watch map[string]bool) func(*daemon.ResourceStatus) bool {
	return func(s *daemon.ResourceStatus) bool { return watch[s.Name] }
}

// commitOnHealthyOrDegraded is the commit predicate shared by every
// "wait for healthy" caller: promote both terminal-success and
// terminal-failure transitions into the renderer's permanent log.
func commitOnHealthyOrDegraded(e progressEvent) bool {
	return e.kind == eventTransition && (e.to == "healthy" || e.to == "degraded")
}

// doneOnHealthy marks a service as completed once its transition
// reaches the healthy state.
func doneOnHealthy(e progressEvent) bool {
	return e.kind == eventTransition && e.to == "healthy"
}

func serviceStartError(name string, snapshot progressSnapshot, evidence string) error {
	if snapshot.portConflict != nil {
		return resourcePortConflictError(snapshot.portConflict)
	}
	message := name + " failed to become healthy"
	if snapshot.reason != "" {
		message += ": " + snapshot.reason
	}
	if evidence != "" && evidence != snapshot.reason {
		message += "\n  Last log: " + evidence
	}
	return fmt.Errorf("%s\n  Next: orbit logs %s", message, name)
}

// coloredEvent applies the existing colour scheme to a commit line so
// healthy/degraded transitions keep their visual weight when committed.
func coloredEvent(e progressEvent, line string) string {
	switch {
	case e.kind == eventTransition && e.to == "healthy":
		return cli.Green.Sprint(line)
	case e.kind == eventTransition && e.to == "degraded":
		return cli.Red.Sprint(line)
	case e.kind == eventTransition && e.to == "stopped":
		return cli.Faint.Sprint(line)
	}
	return line
}

// isPostStopState reports whether a state indicates the stop phase has
// completed (or never started). Empty string is the zero value seen
// when a status response omits an unknown resource — treat as
// indeterminate, NOT past-stop, so the caller keeps polling.
func isPostStopState(state string) bool {
	return state != "" && state != "stopping"
}

// preflightOrAbort runs readiness checks and returns a user-friendly error if
// any block start-up.
func preflightOrAbort(configWithoutManagedEnvironment bool, resourceNames []string) error {
	if !configWithoutManagedEnvironment {
		selected := readCurrentEnv()
		envsDir := envsDestDir()
		if _, _, managed := managedSourceForPath(selected); managed {
			envsDir = filepath.Dir(selected)
		}
		checks := preflight.CheckEnvsReady(envsDir, selected)
		var failures []preflight.Check
		for _, c := range checks {
			if !c.OK {
				failures = append(failures, c)
			}
		}
		if len(failures) > 0 {
			var msg string
			for _, c := range failures {
				msg += fmt.Sprintf("  %s %s: %s\n", cli.Red.Sprint("✗"), c.Name, c.Message)
				if c.Fix != "" {
					msg += fmt.Sprintf("    → %s\n", c.Fix)
				}
			}
			return fmt.Errorf("environment not ready:\n%s", msg)
		}
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		return cli.NewInvalidEnvironmentError("active environment is invalid: " + err.Error())
	}
	if infraOnly {
		return nil
	}
	selected := selectedUpServices(cfg, resourceNames)
	selectedConfig := *cfg
	selectedConfig.Services = make(map[string]*config.Service, len(selected))
	for _, name := range selected {
		selectedConfig.Services[name] = cfg.Services[name]
	}
	checks := daemonsrv.HostEnvironmentChecks(&selectedConfig)
	var failed []daemon.DoctorCheck
	for _, check := range checks {
		if check.Status == daemon.CheckFail {
			failed = append(failed, check)
		}
	}
	if len(failed) == 0 {
		return nil
	}

	response := &daemon.DoctorResponse{Checks: failed}
	actions := doctorRecommendedActions(response)
	for _, check := range failed {
		if strings.HasPrefix(check.Name, "Working directory (") {
			failure := cli.NewServiceWorkingDirectoryError(check.Message)
			return cli.WithJSONReplacementActions(failure, actions)
		}
	}

	var messages []string
	for _, check := range failed {
		message := check.Name + ": " + check.Message
		if check.Hint != "" {
			message += "\n    → " + check.Hint
		}
		messages = append(messages, message)
	}
	failure := cli.NewChecksFailedError("environment not ready:\n  " + strings.Join(messages, "\n  "))
	return cli.WithJSONReplacementActions(failure, actions)
}

func selectedUpServices(cfg *config.Config, resourceNames []string) []string {
	if cfg == nil || infraOnly {
		return []string{}
	}
	selected := make(map[string]bool)
	var visit func(string)
	visit = func(name string) {
		if selected[name] {
			return
		}
		if _, ok := cfg.Services[name]; ok {
			selected[name] = true
		}
		for _, dependency := range cfg.GetDependencies(name) {
			visit(dependency)
		}
	}
	switch {
	case len(groups) > 0:
		for _, groupName := range groups {
			for _, name := range cfg.Groups[groupName].Services {
				visit(name)
			}
		}
	case len(resourceNames) > 0:
		for _, name := range resourceNames {
			visit(name)
		}
	default:
		for name := range cfg.Services {
			selected[name] = true
		}
	}
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
