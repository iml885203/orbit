package app

import (
	"fmt"
	"sort"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/port"
)

const downCompletionMessage = "Environment stopped. Orbit is ready for the next 'orbit up'."
const downAlreadyStoppedMessage = "Environment is already stopped. Orbit is ready for the next 'orbit up'."

type lifecycleJSONOptions struct {
	Operation          string
	Message            string
	RequestedResources []string
	InfraOnly          bool
	FinalStatus        *daemon.StatusResponse
	TimedOutResources  []string
	ContextSwitch      *projectContextSwitch
}

type lifecycleJSONData struct {
	Operation          string                  `json:"operation"`
	Message            string                  `json:"message,omitempty"`
	RequestedResources []string                `json:"requested_resources"`
	InfraOnly          bool                    `json:"infra_only,omitempty"`
	Resources          []daemon.ResourceStatus `json:"resources"`
	DegradedResources  []string                `json:"degraded_resources"`
	TimedOutResources  []string                `json:"timed_out_resources"`
	ContextSwitch      *projectContextSwitch   `json:"context_switch,omitempty"`
}

func buildLifecycleJSONData(opts lifecycleJSONOptions) lifecycleJSONData {
	requestedResources := append([]string{}, opts.RequestedResources...)
	timedOutResources := append([]string{}, opts.TimedOutResources...)
	requested := make(map[string]bool, len(requestedResources))
	for _, name := range requestedResources {
		requested[name] = true
	}
	resources := make([]daemon.ResourceStatus, 0, len(requested))
	degraded := []string{}
	if opts.FinalStatus != nil {
		for i := range opts.FinalStatus.Resources {
			svc := &opts.FinalStatus.Resources[i]
			if len(requested) > 0 && !requested[svc.Name] {
				continue
			}
			resources = append(resources, *svc)
			if svc.State == "degraded" {
				degraded = append(degraded, svc.Name)
			}
		}
	}
	return lifecycleJSONData{
		Operation:          opts.Operation,
		Message:            opts.Message,
		RequestedResources: requestedResources,
		InfraOnly:          opts.InfraOnly,
		Resources:          resources,
		DegradedResources:  degraded,
		TimedOutResources:  timedOutResources,
		ContextSwitch:      opts.ContextSwitch,
	}
}

func lifecycleRecommendedActions(serviceNames []string) []cli.JSONAction {
	actions := []cli.JSONAction{cli.StatusAction()}
	seen := map[string]bool{
		"orbit status --json": true,
	}
	for _, name := range serviceNames {
		for _, action := range []cli.JSONAction{
			{
				Command:     "orbit logs " + name + " --json",
				Reason:      "Inspect recent logs for " + name + ".",
				Destructive: false,
			},
			{
				Command:     "orbit restart " + name + " --json",
				Reason:      "Retry " + name + " after fixing the reported cause.",
				Destructive: false,
			},
		} {
			if seen[action.Command] {
				continue
			}
			actions = append(actions, action)
			seen[action.Command] = true
		}
	}
	return actions
}

func lifecycleDownSuccessActions() []cli.JSONAction {
	return []cli.JSONAction{{
		Command:     "orbit up --json",
		Reason:      "Start the environment when you are ready.",
		Destructive: false,
	}}
}

func lifecycleUpSuccessActions(serviceNames []string, status *daemon.StatusResponse) []cli.JSONAction {
	if resource := primaryOpenableResource(serviceNames, status); resource != nil {
		return []cli.JSONAction{{
			Command:     "orbit open " + resource.Name + " --json",
			Reason:      fmt.Sprintf("Open %s at %s.", resource.Name, resource.URL),
			Destructive: false,
		}}
	}
	return []cli.JSONAction{{
		Command:     "orbit open --json",
		Reason:      "Get the dashboard URL for the healthy environment.",
		Destructive: false,
	}}
}

func primaryOpenableResource(resourceNames []string, status *daemon.StatusResponse) *daemon.ResourceStatus {
	if status == nil {
		return nil
	}
	selected := make(map[string]bool, len(resourceNames))
	for _, name := range resourceNames {
		selected[name] = true
	}
	frontends := make([]*daemon.ResourceStatus, 0)
	selectedCandidates := make([]*daemon.ResourceStatus, 0)
	for i := range status.Resources {
		resource := &status.Resources[i]
		if resource.State != "healthy" || resource.URL == "" {
			continue
		}
		if resource.Role == "frontend" {
			frontends = append(frontends, resource)
		}
		if len(selected) == 0 || selected[resource.Name] {
			selectedCandidates = append(selectedCandidates, resource)
		}
	}
	sortOpenableResources(frontends)
	sortOpenableResources(selectedCandidates)
	for _, resource := range frontends {
		if selected[resource.Name] {
			return resource
		}
	}
	if len(frontends) == 1 {
		return frontends[0]
	}
	if len(selectedCandidates) == 0 {
		return nil
	}
	return selectedCandidates[0]
}

func sortOpenableResources(resources []*daemon.ResourceStatus) {
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Role != resources[j].Role {
			return resources[i].Role == "frontend"
		}
		if resources[i].Kind != resources[j].Kind {
			return resources[i].Kind == daemon.ResourceKindService
		}
		return resources[i].Name < resources[j].Name
	})
}

func lifecycleRecommendedActionsForStatus(serviceNames []string, status *daemon.StatusResponse) []cli.JSONAction {
	if status == nil {
		return lifecycleRecommendedActions(serviceNames)
	}
	byName := make(map[string]*daemon.ResourceStatus, len(status.Resources))
	for i := range status.Resources {
		service := &status.Resources[i]
		byName[service.Name] = service
	}
	terminalNames := make([]string, 0, len(serviceNames))
	for _, name := range serviceNames {
		service := byName[name]
		if service == nil {
			continue
		}
		if service.PortConflict != nil {
			return resourcePortConflictActions(service.PortConflict)
		}
		switch service.State {
		case "degraded":
			if service.HealthProgress == nil || !service.HealthProgress.Recovering {
				terminalNames = append(terminalNames, service.Name)
			}
		case "stopped":
			terminalNames = append(terminalNames, service.Name)
		case "pending":
			if blocker := terminalDependencyBlocker(status, service.PendingDependencies); blocker != nil {
				if blocker.PortConflict != nil {
					return resourcePortConflictActions(blocker.PortConflict)
				}
				terminalNames = append(terminalNames, blocker.Name)
			}
		}
	}
	if len(terminalNames) > 0 {
		var actions []cli.JSONAction
		seen := make(map[string]bool, len(terminalNames))
		for _, name := range terminalNames {
			if seen[name] {
				continue
			}
			seen[name] = true
			service := byName[name]
			if service != nil && service.LogsAvailable {
				actions = append(actions, cli.JSONAction{
					Command:     "orbit logs " + name + " --json",
					Reason:      "Inspect the failure evidence for " + name + " before choosing a recovery.",
					Destructive: false,
				})
				continue
			}
			actions = append(actions, cli.JSONAction{
				Command:     "orbit restart " + name + " --json",
				Reason:      "Retry only " + name + "; it produced no output to inspect.",
				Destructive: false,
			})
		}
		return actions
	}

	evidenceNames := make([]string, 0, len(serviceNames))
	for _, name := range serviceNames {
		service := byName[name]
		if service == nil {
			continue
		}
		evidence := lifecycleEvidenceResource(service, byName, make(map[string]bool, len(byName)))
		if evidence == nil {
			continue
		}
		evidenceNames = append(evidenceNames, evidence.Name)
	}
	return lifecycleRecommendedActions(evidenceNames)
}

func resourcePortConflictActions(conflict *daemon.ResourcePortConflict) []cli.JSONAction {
	if conflict == nil || conflict.InspectCommand == "" {
		return nil
	}
	return []cli.JSONAction{{
		Command:     conflict.InspectCommand,
		Reason:      fmt.Sprintf("Inspect the process using port %d required by %s.", conflict.Port, conflict.Resource),
		Destructive: false,
	}}
}

func noLogsRecoveryActions(resource *daemon.ResourceStatus) []cli.JSONAction {
	if resource == nil {
		return nil
	}
	if resource.PortConflict != nil {
		conflicts := port.CheckPorts(map[string][]int{
			resource.Name: {resource.PortConflict.Port},
		})
		if len(conflicts) > 0 {
			current := port.NewConflictError(conflicts[0])
			return resourcePortConflictActions(&daemon.ResourcePortConflict{
				Port:           current.Port,
				Resource:       current.Service,
				PID:            current.PID,
				Process:        current.Process,
				InspectCommand: current.InspectCommand,
			})
		}
	}
	return []cli.JSONAction{{
		Command:     "orbit up " + resource.Name + " --json",
		Reason:      "Retry " + resource.Name + "; no process output exists because it did not start.",
		Destructive: false,
	}}
}

func logsRecoveryActions(resource *daemon.ResourceStatus, dependencySetup string) []cli.JSONAction {
	if resource == nil || resource.State != "degraded" {
		return []cli.JSONAction{cli.StatusAction()}
	}
	if resource.PortConflict != nil {
		if actions := resourcePortConflictActions(resource.PortConflict); len(actions) > 0 {
			return actions
		}
	}
	if resource.HealthProgress != nil && resource.HealthProgress.Recovering {
		return []cli.JSONAction{cli.StatusAction()}
	}
	if dependencySetup != "" {
		return []cli.JSONAction{{
			Command:     dependencySetup + " && orbit restart " + resource.Name + " --json",
			Reason:      "Install the declared project dependencies, then retry only " + resource.Name + ".",
			Destructive: false,
		}}
	}
	if resource.FailureKind == string(engine.FailureKindHealth) {
		return []cli.JSONAction{{
			Command:     "orbit restart " + resource.Name + " --json",
			Reason:      "Retry the health probe after addressing its cause; restarting does not repair a persistent health failure.",
			Destructive: false,
		}}
	}
	return []cli.JSONAction{{
		Command:     "orbit restart " + resource.Name + " --json",
		Reason:      "Retry " + resource.Name + " after reviewing its exit output.",
		Destructive: false,
	}}
}

func lifecycleEvidenceResource(service *daemon.ResourceStatus, byName map[string]*daemon.ResourceStatus, visited map[string]bool) *daemon.ResourceStatus {
	if service == nil || service.State == "healthy" || visited[service.Name] {
		return nil
	}
	visited[service.Name] = true
	if service.State != "pending" {
		return service
	}
	for _, dependencyName := range service.PendingDependencies {
		dependency := byName[dependencyName]
		if dependency == nil || dependency.State == "healthy" {
			continue
		}
		if evidence := lifecycleEvidenceResource(dependency, byName, visited); evidence != nil {
			return evidence
		}
	}
	return service
}

func waitForLifecycleJSON(client *daemon.Client, names []string, wantState string) (*daemon.StatusResponse, error) {
	return waitForLifecycleJSONOrPast(client, names, wantState, nil)
}

func waitForLifecycleRestartHealthyJSON(client *daemon.Client, names []string) (*daemon.StatusResponse, error) {
	return waitForLifecycleJSONOrPastWithTerminal(client, names, "healthy", nil, true)
}

func waitForLifecycleJSONOrPast(client *daemon.Client, names []string, wantState string, pastState func(string) bool) (*daemon.StatusResponse, error) {
	return waitForLifecycleJSONOrPastWithTerminal(client, names, wantState, pastState, true)
}

func waitForLifecycleJSONOrPastWithTerminal(client *daemon.Client, names []string, wantState string, pastState func(string) bool, failStopped bool) (*daemon.StatusResponse, error) {
	if len(names) == 0 {
		return client.Status()
	}
	deadline := time.After(effectiveTimeout(timeout))
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var last *daemon.StatusResponse
	// prevStates lets the stopped-wait distinguish "stop failed just now"
	// (stopping → degraded, terminal — waiting longer only burns the
	// timeout) from a service that was already degraded before the stop
	// began (stops normally). Only pure stop waits fail fast; restart's
	// stop phase (pastState != nil) re-enters pending even on stop errors.
	prevStates := map[string]string{}
	for {
		select {
		case <-deadline:
			return last, cli.NewTimeoutError(fmt.Sprintf("timeout waiting for resources to become %s", wantState))
		case <-ticker.C:
			status, err := client.Status()
			if err != nil {
				return last, err
			}
			last = status
			if wantState == "healthy" {
				if err := lifecycleTerminalError(client, status, names, failStopped); err != nil {
					return last, err
				}
			}
			if wantState == "stopped" && pastState == nil {
				if err := lifecycleStopFailedError(status, names, prevStates); err != nil {
					return last, err
				}
			}
			if lifecycleServicesDoneOrPast(status, names, wantState, pastState) {
				return status, nil
			}
			for i := range status.Resources {
				prevStates[status.Resources[i].Name] = status.Resources[i].State
			}
		}
	}
}

// lifecycleStopFailedError reports a terminal error when a watched service
// just moved stopping → degraded, which is how StopService records a failed
// stop. prevStates must hold the previous tick's states so a service that
// was degraded before the stop began is not misread as a stop failure.
func lifecycleStopFailedError(status *daemon.StatusResponse, names []string, prevStates map[string]string) error {
	watch := watchSet(names)
	for i := range status.Resources {
		s := &status.Resources[i]
		if !watch[s.Name] || s.State != "degraded" || prevStates[s.Name] != "stopping" {
			continue
		}
		reason := s.StateReason
		if reason == "" {
			reason = "stop failed"
		}
		return fmt.Errorf("stop failed for %s: %s — check 'orbit status' and 'docker ps'", s.Name, reason)
	}
	return nil
}

func waitForLifecycleRestartObserved(client *daemon.Client, name string, priorRestartCount *int) (*daemon.StatusResponse, error) {
	deadline := time.After(effectiveTimeout(timeout))
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var last *daemon.StatusResponse
	for {
		select {
		case <-deadline:
			return last, cli.NewTimeoutError(fmt.Sprintf("timeout waiting for %s restart to begin", name))
		case <-ticker.C:
			status, err := client.Status()
			if err != nil {
				return last, err
			}
			last = status
			if lifecycleRestartObserved(status, name, priorRestartCount) {
				return status, nil
			}
		}
	}
}

func lifecycleTerminalError(client *daemon.Client, status *daemon.StatusResponse, names []string, failStopped bool) error {
	if status == nil {
		return nil
	}
	watched := make(map[string]bool, len(names))
	for _, name := range names {
		watched[name] = true
	}
	for i := range status.Resources {
		svc := &status.Resources[i]
		if len(watched) > 0 && !watched[svc.Name] {
			continue
		}
		switch svc.State {
		case "degraded":
			// Health recovery may still flip this service back on its own;
			// only a degraded with no recovery probing running is terminal.
			if svc.HealthProgress != nil && svc.HealthProgress.Recovering {
				continue
			}
			if svc.PortConflict != nil {
				return resourcePortConflictError(svc.PortConflict)
			}
			reason := svc.StateReason
			if reason == "" && svc.HealthProgress != nil {
				reason = svc.HealthProgress.LastErr
			}
			message := svc.Name + " failed to become healthy"
			if reason != "" {
				message += ": " + reason
			}
			if evidence := recentLogEvidence(client, svc.Name); evidence != "" && evidence != reason {
				message += "\nLast log: " + evidence
			}
			return cli.NewServiceStartFailedError(message)
		case "pending":
			dependency := terminalDependencyBlocker(status, svc.PendingDependencies)
			if dependency == nil {
				continue
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
			message := fmt.Sprintf("%s cannot start because dependency %s is unhealthy", svc.Name, dependency.Name)
			if reason != "" {
				message += ": " + reason
			}
			return cli.NewDependencyBlockedError(message)
		case "stopped":
			if failStopped {
				return fmt.Errorf("%s stopped before becoming healthy", svc.Name)
			}
		}
	}
	return nil
}

func resourcePortConflictError(conflict *daemon.ResourcePortConflict) error {
	if conflict == nil {
		return nil
	}
	return &cli.ResourcePortConflictError{
		Port:           conflict.Port,
		Resource:       conflict.Resource,
		PID:            conflict.PID,
		Process:        conflict.Process,
		InspectCommand: conflict.InspectCommand,
	}
}

func terminalDependencyBlocker(status *daemon.StatusResponse, pendingDependencies []string) *daemon.ResourceStatus {
	if status == nil {
		return nil
	}
	byName := make(map[string]*daemon.ResourceStatus, len(status.Resources))
	for i := range status.Resources {
		byName[status.Resources[i].Name] = &status.Resources[i]
	}
	visited := make(map[string]bool, len(status.Resources))
	var find func([]string) *daemon.ResourceStatus
	find = func(names []string) *daemon.ResourceStatus {
		for _, name := range names {
			if visited[name] {
				continue
			}
			visited[name] = true
			service := byName[name]
			if service == nil {
				continue
			}
			switch service.State {
			case "stopped":
				return service
			case "degraded":
				if service.HealthProgress == nil || !service.HealthProgress.Recovering {
					return service
				}
			case "pending":
				if blocker := find(service.PendingDependencies); blocker != nil {
					return blocker
				}
			}
		}
		return nil
	}
	return find(pendingDependencies)
}

func lifecycleServicesDone(status *daemon.StatusResponse, names []string, wantState string) bool {
	return lifecycleServicesDoneOrPast(status, names, wantState, nil)
}

func lifecycleResourceExists(status *daemon.StatusResponse, name string) bool {
	return lifecycleResourceStatus(status, name) != nil
}

func lifecycleResourceStatus(status *daemon.StatusResponse, name string) *daemon.ResourceStatus {
	if status == nil {
		return nil
	}
	for i := range status.Resources {
		svc := &status.Resources[i]
		if svc.Name == name {
			return svc
		}
	}
	return nil
}

func lifecycleRestartCount(status *daemon.StatusResponse, name string) *int {
	if status == nil {
		return nil
	}
	for i := range status.Resources {
		svc := &status.Resources[i]
		if svc.Name == name {
			count := svc.RestartCount
			return &count
		}
	}
	return nil
}

// lifecycleRestartObserved reports whether a restart of `name` has actually
// happened since priorRestartCount was sampled. Reaching state=healthy is not
// sufficient — a stale snapshot from before the restart could already be
// healthy. The RestartCount delta is the only signal that proves the
// orchestrator observed the restart cycle. See
// TestLifecycleRestartObservedRejectsStaleHealthyWithUnchangedRestartCount.
func lifecycleRestartObserved(status *daemon.StatusResponse, name string, priorRestartCount *int) bool {
	if status == nil {
		return false
	}
	for i := range status.Resources {
		svc := &status.Resources[i]
		if svc.Name != name {
			continue
		}
		if priorRestartCount != nil {
			return svc.RestartCount > *priorRestartCount
		}
		return svc.State != "healthy"
	}
	return false
}

func lifecycleServicesDoneOrPast(status *daemon.StatusResponse, names []string, wantState string, pastState func(string) bool) bool {
	if status == nil {
		return false
	}
	byName := make(map[string]string, len(status.Resources))
	for i := range status.Resources {
		svc := &status.Resources[i]
		byName[svc.Name] = svc.State
	}
	for _, name := range names {
		state, ok := byName[name]
		if !ok {
			return false
		}
		if state == wantState {
			continue
		}
		if pastState == nil || !pastState(state) {
			return false
		}
	}
	return true
}

// lifecycleRestartPastStopState reports whether a service has moved past the
// transient "stopped" snapshot taken between stop and start during a restart.
// All five active states qualify because the orchestrator can transition
// through them faster than our poll cadence — if we required state=="stopped"
// a fast restart would race past it and we'd block until the watchdog timeout.
func lifecycleRestartPastStopState(state string) bool {
	switch state {
	case "pending", "starting", "building", "healthy", "degraded":
		return true
	default:
		return false
	}
}
