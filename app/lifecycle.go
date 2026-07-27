package app

import (
	"fmt"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

type lifecycleJSONOptions struct {
	Operation         string
	Message           string
	RequestedServices []string
	InfraOnly         bool
	FinalStatus       *daemon.StatusResponse
	TimedOutServices  []string
}

type lifecycleJSONData struct {
	Operation         string                 `json:"operation"`
	Message           string                 `json:"message,omitempty"`
	RequestedServices []string               `json:"requested_services"`
	InfraOnly         bool                   `json:"infra_only,omitempty"`
	Services          []daemon.ServiceStatus `json:"services"`
	DegradedServices  []string               `json:"degraded_services"`
	TimedOutServices  []string               `json:"timed_out_services"`
}

func buildLifecycleJSONData(opts lifecycleJSONOptions) lifecycleJSONData {
	requestedServices := append([]string{}, opts.RequestedServices...)
	timedOutServices := append([]string{}, opts.TimedOutServices...)
	requested := make(map[string]bool, len(requestedServices))
	for _, name := range requestedServices {
		requested[name] = true
	}
	services := make([]daemon.ServiceStatus, 0, len(requested))
	degraded := []string{}
	if opts.FinalStatus != nil {
		for i := range opts.FinalStatus.Services {
			svc := &opts.FinalStatus.Services[i]
			if len(requested) > 0 && !requested[svc.Name] {
				continue
			}
			services = append(services, *svc)
			if svc.State == "degraded" {
				degraded = append(degraded, svc.Name)
			}
		}
	}
	return lifecycleJSONData{
		Operation:         opts.Operation,
		Message:           opts.Message,
		RequestedServices: requestedServices,
		InfraOnly:         opts.InfraOnly,
		Services:          services,
		DegradedServices:  degraded,
		TimedOutServices:  timedOutServices,
	}
}

func lifecycleRecommendedActions(serviceNames []string) []cli.JSONAction {
	actions := []cli.JSONAction{cli.StatusAction(), cli.DoctorAction()}
	seen := map[string]bool{
		"orbit status --json": true,
		"orbit doctor --json": true,
	}
	for _, name := range serviceNames {
		cmd := "orbit logs " + name + " --json"
		if seen[cmd] {
			continue
		}
		actions = append(actions, cli.JSONAction{
			Command:     cmd,
			Reason:      "Inspect recent logs for " + name + ".",
			Destructive: false,
		})
		seen[cmd] = true
	}
	return actions
}

func lifecycleRecommendedActionsForStatus(serviceNames []string, status *daemon.StatusResponse) []cli.JSONAction {
	if status == nil {
		return lifecycleRecommendedActions(serviceNames)
	}
	watched := watchSet(serviceNames)
	evidenceNames := make([]string, 0, len(serviceNames))
	blockedBy := make(map[string]string)
	for i := range status.Services {
		service := &status.Services[i]
		if !watched[service.Name] {
			continue
		}
		if service.State != "pending" {
			evidenceNames = append(evidenceNames, service.Name)
			continue
		}
		dependency := terminalDependencyBlocker(status, service.PendingDependencies)
		if dependency == nil {
			evidenceNames = append(evidenceNames, service.Name)
			continue
		}
		evidenceNames = append(evidenceNames, dependency.Name)
		blockedBy[dependency.Name] = service.Name
	}
	actions := lifecycleRecommendedActions(evidenceNames)
	for _, dependency := range sortedKeys(blockedBy) {
		dependent := blockedBy[dependency]
		actions = cli.MergeActions(actions, []cli.JSONAction{
			{
				Command:     "orbit restart " + dependency + " --json",
				Reason:      "Restore the dependency blocking " + dependent + ".",
				Destructive: false,
			},
		})
	}
	return actions
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
			return last, cli.NewTimeoutError(fmt.Sprintf("timeout waiting for services to become %s", wantState))
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
			for i := range status.Services {
				prevStates[status.Services[i].Name] = status.Services[i].State
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
	for i := range status.Services {
		s := &status.Services[i]
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
	for i := range status.Services {
		svc := &status.Services[i]
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

func terminalDependencyBlocker(status *daemon.StatusResponse, pendingDependencies []string) *daemon.ServiceStatus {
	if status == nil {
		return nil
	}
	byName := make(map[string]*daemon.ServiceStatus, len(status.Services))
	for i := range status.Services {
		byName[status.Services[i].Name] = &status.Services[i]
	}
	visited := make(map[string]bool, len(status.Services))
	var find func([]string) *daemon.ServiceStatus
	find = func(names []string) *daemon.ServiceStatus {
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

func lifecycleServiceExists(status *daemon.StatusResponse, name string) bool {
	if status == nil {
		return false
	}
	for i := range status.Services {
		svc := &status.Services[i]
		if svc.Name == name {
			return true
		}
	}
	return false
}

func lifecycleRestartCount(status *daemon.StatusResponse, name string) *int {
	if status == nil {
		return nil
	}
	for i := range status.Services {
		svc := &status.Services[i]
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
	for i := range status.Services {
		svc := &status.Services[i]
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
	byName := make(map[string]string, len(status.Services))
	for i := range status.Services {
		svc := &status.Services[i]
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
