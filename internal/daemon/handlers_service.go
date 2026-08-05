package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/logging"
)

// handleHealth responds with {OK:true} for liveness probes.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, APIResponse{OK: true})
}

// handleStatus returns the current status of every configured service
// and container, merging tracked orchestrator state with config-only
// entries so stopped services still appear.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.environmentTransitionMu.RLock()
	defer s.environmentTransitionMu.RUnlock()
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	statuses := s.computeStatuses(s.holder.Load())
	stale, staleReason := s.configStale()
	resp := StatusResponse{
		Epoch:             s.epoch(),
		Resources:         statuses,
		ConfigPath:        s.ConfigPath(),
		Context:           s.environmentContext(),
		Instance:          s.instanceName,
		ConfigStale:       stale,
		ConfigStaleReason: staleReason,
	}
	writeJSON(w, http.StatusOK, resp)
}

// computeStatuses assembles the current ResourceStatus for every service and
// container tracked by the orchestrator, preserving GetAllServices order.
// computeStatuses assembles ResourceStatus views against the caller's config
// snapshot — the caller owns the one-Load-per-operation boundary, so a
// handler that also builds other views (graph, resources) renders
// everything from a single generation.
func (s *Server) computeStatuses(cfg *config.Config) []ResourceStatus {
	services := s.app.Orchestrator.GetAllServices()
	out := make([]ResourceStatus, 0, len(services))
	for i := range services {
		svc := &services[i]
		ports := getServicePorts(cfg, svc.Name, svc.Kind)
		url := ""
		if svcCfg, ok := cfg.Services[svc.Name]; ok {
			url = svcCfg.ResolveURL()
		}
		startupTime := ""
		uptime := ""
		if !svc.StartedAt.IsZero() && !svc.HealthyAt.IsZero() {
			startupTime = formatDuration(svc.HealthyAt.Sub(svc.StartedAt))
		}
		uptimeFrom := svc.HealthyAt
		if svc.Kind == "container" && !svc.ContainerStartedAt.IsZero() {
			uptimeFrom = svc.ContainerStartedAt
		}
		if !uptimeFrom.IsZero() && svc.State == engine.StateHealthy && !svc.ExpectingContainerStart {
			uptime = formatDuration(time.Since(uptimeFrom))
		}
		lastRestart := resourceLastRestart(svc)
		sidecars := getSidecarInfos(cfg, svc.Name, svc.Kind)
		mode := ""
		if isDualDefined(cfg, svc.Name) {
			mode = s.settings.GetServiceMode(svc.Name)
		}
		image := getContainerImage(cfg, svc.Name, svc.Kind)
		var hp *HealthProgressInfo
		if p := s.app.HealthChecker.Progress(svc.Name); p.Configured && p.Attempts > 0 {
			hp = &HealthProgressInfo{
				Attempts:   p.Attempts,
				MaxRetries: p.MaxRetries,
				LastErr:    p.LastErr,
				Recovering: p.Recovering,
			}
		}
		pendingDependencies := make([]string, 0, len(svc.PendingDeps))
		for dependency := range svc.PendingDeps {
			pendingDependencies = append(pendingDependencies, dependency)
		}
		sort.Strings(pendingDependencies)
		out = append(out, ResourceStatus{
			Name:                 svc.Name,
			Kind:                 ResourceKind(svc.Kind),
			Role:                 getResourceRole(cfg, svc.Name, svc.Kind),
			State:                svc.State.String(),
			PendingDependencies:  pendingDependencies,
			StateReason:          svc.StateReason,
			FailureKind:          string(svc.FailureKind),
			FailureEvidence:      svc.FailureEvidence,
			PortConflict:         resourcePortConflict(svc),
			LogsAvailable:        resourceLogsAvailable(s.app.Logs, svc.Name),
			RestartCount:         svc.RestartCount,
			ExternalRestartCount: svc.ExternalRestartCount,
			LastRestart:          lastRestart,
			Ports:                ports,
			URL:                  url,
			Image:                image,
			StartupTime:          startupTime,
			Uptime:               uptime,
			Sidecars:             sidecars,
			Mode:                 mode,
			HealthProgress:       hp,
		})
	}
	applyDependencyImpact(s.app.Orchestrator.DepGraph(), out)
	return out
}

// applyDependencyImpact overlays availability on top of lifecycle truth.
// A process may still be alive while a required dependency is unavailable;
// clients need that fact without mutating the orchestrator's lifecycle state.
func applyDependencyImpact(dependencies *engine.DepGraph, statuses []ResourceStatus) {
	index := make(map[string]int, len(statuses))
	for i := range statuses {
		index[statuses[i].Name] = i
	}
	visited := make(map[string]bool, len(statuses))
	visiting := make(map[string]bool, len(statuses))
	var visit func(string)
	visit = func(name string) {
		if visited[name] || visiting[name] {
			return
		}
		visiting[name] = true
		for _, dependency := range dependencies.DepsOf(name) {
			visit(dependency)
		}
		delete(visiting, name)
		visited[name] = true

		position, ok := index[name]
		if !ok {
			return
		}
		switch statuses[position].State {
		case engine.StateHealthy.String(), engine.StateDegraded.String():
		default:
			return
		}
		for _, dependency := range dependencies.DepsOf(name) {
			dependencyPosition, exists := index[dependency]
			if !exists || statuses[dependencyPosition].State == engine.StateHealthy.String() {
				continue
			}
			statuses[position].State = engine.StateDegraded.String()
			statuses[position].BlockedBy = dependency
			statuses[position].StateReason = fmt.Sprintf(
				"dependency %s is %s",
				dependency,
				statuses[dependencyPosition].State,
			)
			statuses[position].Uptime = ""
			return
		}
	}
	for name := range index {
		visit(name)
	}
}

func resourceLastRestart(svc *engine.ServiceInfo) *ResourceRestart {
	if svc == nil || svc.LastExternalRestart.IsZero() {
		return nil
	}
	return &ResourceRestart{
		Source: "external", StartedAt: svc.LastExternalStartedAt,
		ObservedAt: svc.LastExternalRestart,
	}
}

func resourceLogsAvailable(logs *logging.Multiplexer, name string) bool {
	buffer := logs.GetBuffer(name)
	return buffer != nil && buffer.Count() > 0
}

func resourcePortConflict(svc *engine.ServiceInfo) *ResourcePortConflict {
	if svc == nil || svc.PortConflict == nil {
		return nil
	}
	return &ResourcePortConflict{
		Port:           svc.PortConflict.Port,
		Resource:       svc.PortConflict.Service,
		PID:            svc.PortConflict.PID,
		Process:        svc.PortConflict.Process,
		InspectCommand: svc.PortConflict.InspectCommand,
	}
}

func (s *Server) handleUp(w http.ResponseWriter, r *http.Request) {
	s.environmentTransitionMu.Lock()
	defer s.environmentTransitionMu.Unlock()
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	var req UpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid request body"})
		return
	}

	if req.InfraOnly {
		containerNames := sortedKeys(s.holder.Load().Containers)
		s.app.StartServices(containerNames)
		writeJSON(w, http.StatusOK, APIResponse{
			OK:                true,
			Message:           upStartMessage(req, containerNames),
			AffectedResources: containerNames,
		})
		return
	}

	names, err := s.resolveServicesFromRequest(req)
	if err != nil {
		code := ""
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, errUnknownGroup):
			code = "unknown_group"
		case errors.Is(err, errUnknownResource):
			code = apiCodeUnknownResource
			status = http.StatusNotFound
		}
		writeJSON(w, status, APIResponse{Error: err.Error(), Code: code})
		return
	}

	sort.Strings(names)
	s.app.StartServices(names)
	writeJSON(w, http.StatusOK, APIResponse{
		OK:                true,
		Message:           upStartMessage(req, names),
		AffectedResources: names,
	})
}

func upStartMessage(req UpRequest, affected []string) string {
	if len(affected) == 0 {
		switch {
		case req.InfraOnly:
			return "No containers are configured for this environment."
		case len(req.Groups) > 0:
			return fmt.Sprintf("No resources match the selected groups: %s.", strings.Join(req.Groups, ", "))
		default:
			return "No resources are enabled for this environment."
		}
	}
	switch {
	case req.InfraOnly:
		return fmt.Sprintf("Starting infrastructure (%s).", countNoun(len(affected), "container"))
	case len(req.Resources) == 1:
		dependencies := len(affected) - 1
		if dependencies == 0 {
			return fmt.Sprintf("Starting %s.", req.Resources[0])
		}
		return fmt.Sprintf("Starting %s with %s.", req.Resources[0], countNoun(dependencies, "dependency"))
	case len(req.Resources) > 1:
		requested := uniqueCount(req.Resources)
		dependencies := len(affected) - requested
		if dependencies == 0 {
			return fmt.Sprintf("Starting %s.", countNoun(requested, "requested resource"))
		}
		return fmt.Sprintf(
			"Starting %s with %s.",
			countNoun(requested, "requested resource"),
			countNoun(dependencies, "dependency"),
		)
	case len(req.Groups) == 1:
		return fmt.Sprintf("Starting group %s (%s).", req.Groups[0], countNoun(len(affected), "resource"))
	case len(req.Groups) > 1:
		return fmt.Sprintf(
			"Starting %s (%s).",
			countNoun(len(req.Groups), "group"),
			countNoun(len(affected), "resource"),
		)
	default:
		return fmt.Sprintf("Starting environment (%s).", countNoun(len(affected), "resource"))
	}
}

func countNoun(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	switch noun {
	case "dependency":
		return fmt.Sprintf("%d dependencies", count)
	default:
		return fmt.Sprintf("%d %ss", count, noun)
	}
}

func uniqueCount(values []string) int {
	unique := make(map[string]bool, len(values))
	for _, value := range values {
		unique[value] = true
	}
	return len(unique)
}

const apiCodeUnknownResource = "unknown_resource"

var (
	errUnknownGroup    = errors.New("unknown group")
	errUnknownResource = errors.New("unknown resource")
)

func unknownResourceError(name string) error {
	return fmt.Errorf("%w: %s", errUnknownResource, name)
}

func writeUnknownResource(w http.ResponseWriter, name string) {
	writeJSON(w, http.StatusNotFound, APIResponse{
		Error: unknownResourceError(name).Error(),
		Code:  apiCodeUnknownResource,
	})
}

func (s *Server) resolveServicesFromRequest(req UpRequest) ([]string, error) {
	if len(req.Resources) > 0 {
		return s.resolveExplicitServices(req.Resources)
	}
	cfg := s.holder.Load()
	if err := validateRequestedGroups(cfg, req.Groups); err != nil {
		return nil, err
	}
	detached := s.settings.GetDetachedEdges(s.currentEnvName())
	enabled := engine.FilterEnabledServicesWithDetached(cfg, req.Groups, detached)
	names := make([]string, 0, len(enabled))
	for name := range enabled {
		names = append(names, name)
	}
	return names, nil
}

func validateRequestedGroups(cfg *config.Config, groups []string) error {
	unknown := make([]string, 0, len(groups))
	for _, name := range groups {
		if _, exists := cfg.Groups[name]; !exists {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	message := strings.Join(unknown, ", ")
	available := sortedKeys(cfg.Groups)
	if len(available) == 0 {
		return fmt.Errorf("%w: %s; this environment defines no groups", errUnknownGroup, message)
	}
	return fmt.Errorf("%w: %s; available groups: %s", errUnknownGroup, message, strings.Join(available, ", "))
}

func (s *Server) resolveExplicitServices(services []string) ([]string, error) {
	detached := s.settings.GetDetachedEdges(s.currentEnvName())
	cfg := s.holder.Load()
	for _, name := range services {
		if !cfg.ServiceOrContainerExists(name) {
			return nil, unknownResourceError(name)
		}
	}

	toAdd := make(map[string]bool, len(services))
	for _, name := range services {
		toAdd[name] = true
	}
	engine.AddDepsWithDetached(cfg, toAdd, detached)

	names := make([]string, 0, len(toAdd))
	for name := range toAdd {
		names = append(names, name)
	}
	return names, nil
}

func (s *Server) handleDown(w http.ResponseWriter, r *http.Request) {
	s.environmentTransitionMu.Lock()
	defer s.environmentTransitionMu.Unlock()
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	generation := s.environmentGeneration.Load()
	var req DownRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req = DownRequest{}
	}

	if req.All {
		// Stop dev services and exit daemon. Containers are left running —
		// use `orbit down` first if you want them stopped.
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: "shutting down daemon"})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		s.startEnvironmentBackground(generation, func() {
			time.Sleep(200 * time.Millisecond)
			// Feature OnDown hooks run FIRST, before any teardown. Tunnel
			// release depends on this ordering: releasing before ctx
			// cancel lets the gateway drop the leases immediately
			// instead of waiting for lease expiry.
			for _, onDown := range s.extHooks.OnDown {
				onDown()
			}
			s.app.ShutdownServices()
			_ = s.app.ContainerMgr.Close()
			s.stateFile.Remove()
			Cleanup()
			if s.cancelFunc != nil {
				s.cancelFunc()
			}
		})
		return
	}

	if req.Wait {
		s.app.StopAllServices()
		s.PersistState()
		s.stateFile.Remove()
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: "stopped all services and containers"})
		return
	}

	if downRequestHasSelection(req) {
		names, err := s.resolveStopSelection(req)
		if err != nil {
			code := ""
			status := http.StatusBadRequest
			switch {
			case errors.Is(err, errUnknownGroup):
				code = "unknown_group"
			case errors.Is(err, errUnknownResource):
				code = apiCodeUnknownResource
				status = http.StatusNotFound
			}
			writeJSON(w, status, APIResponse{Error: err.Error(), Code: code})
			return
		}
		s.startEnvironmentBackground(generation, func() {
			s.app.StopServices(names)
			s.PersistState()
		})
		writeJSON(w, http.StatusOK, APIResponse{
			OK:                true,
			Message:           downStopMessage(req, names),
			AffectedResources: names,
		})
		return
	}

	// Stop all services and containers in parallel through the canonical
	// StopService lifecycle. Status pollers (orbit down's progress
	// renderer) see real stopping → stopped transitions instead of a
	// sudden state jump at the end.
	s.startEnvironmentBackground(generation, func() {
		s.app.StopAllServices()
		s.PersistState()
		s.stateFile.Remove()
	})
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: "stopping all services and containers"})
}

func downRequestHasSelection(req DownRequest) bool {
	return len(req.Resources) > 0 || len(req.Groups) > 0 || req.InfraOnly
}

func (s *Server) resolveStopSelection(req DownRequest) ([]string, error) {
	selectedModes := 0
	for _, selected := range []bool{len(req.Resources) > 0, len(req.Groups) > 0, req.InfraOnly} {
		if selected {
			selectedModes++
		}
	}
	if selectedModes > 1 {
		return nil, fmt.Errorf("resource names, --group, and --infra are separate selection modes")
	}

	cfg := s.holder.Load()
	selected := make(map[string]bool)
	switch {
	case len(req.Resources) > 0:
		for _, name := range req.Resources {
			if !cfg.ServiceOrContainerExists(name) {
				return nil, unknownResourceError(name)
			}
			selected[name] = true
		}
	case len(req.Groups) > 0:
		if err := validateRequestedGroups(cfg, req.Groups); err != nil {
			return nil, err
		}
		for _, groupName := range req.Groups {
			for _, name := range cfg.Groups[groupName].Services {
				selected[name] = true
			}
		}
	case req.InfraOnly:
		for name := range cfg.Containers {
			selected[name] = true
		}
	}
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func downStopMessage(req DownRequest, affected []string) string {
	if len(affected) == 0 {
		switch {
		case req.InfraOnly:
			return "No containers are configured for this environment."
		case len(req.Groups) > 0:
			return fmt.Sprintf("No resources match the selected groups: %s.", strings.Join(req.Groups, ", "))
		default:
			return "No resources were selected."
		}
	}
	switch {
	case req.InfraOnly:
		return fmt.Sprintf("Stopping infrastructure (%s).", countNoun(len(affected), "container"))
	case len(req.Resources) == 1:
		return fmt.Sprintf("Stopping %s.", req.Resources[0])
	case len(req.Resources) > 1:
		return fmt.Sprintf("Stopping %s.", countNoun(len(affected), "requested resource"))
	case len(req.Groups) == 1:
		return fmt.Sprintf("Stopping group %s (%s).", req.Groups[0], countNoun(len(affected), "resource"))
	default:
		return fmt.Sprintf("Stopping %s (%s).", countNoun(len(req.Groups), "group"), countNoun(len(affected), "resource"))
	}
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	s.environmentTransitionMu.Lock()
	defer s.environmentTransitionMu.Unlock()
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	generation := s.environmentGeneration.Load()
	name := strings.TrimPrefix(r.URL.Path, "/api/stop/")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "resource name required"})
		return
	}
	if !s.holder.Load().ServiceOrContainerExists(name) {
		writeUnknownResource(w, name)
		return
	}

	s.startEnvironmentBackground(generation, func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.holder.Load().Settings.ShutdownTimeout)
		defer cancel()
		if err := s.app.StopService(ctx, name); err != nil {
			slog.Error("stop failed", "component", "stop", "name", name, "err", err)
		}
		s.PersistState()
	})

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: fmt.Sprintf("stopping %s", name)})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	s.environmentTransitionMu.Lock()
	defer s.environmentTransitionMu.Unlock()
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	generation := s.environmentGeneration.Load()
	name := strings.TrimPrefix(r.URL.Path, "/api/restart/")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "resource name required"})
		return
	}
	if !s.holder.Load().ServiceOrContainerExists(name) {
		writeUnknownResource(w, name)
		return
	}

	s.startEnvironmentBackground(generation, func() {
		// RestartService's ctx covers only the stop phase (start is
		// driven by the orchestrator event loop afterwards), so reuse
		// the same ShutdownTimeout handleStop and handleDown use rather
		// than picking an arbitrary longer deadline.
		ctx, cancel := context.WithTimeout(context.Background(), s.holder.Load().Settings.ShutdownTimeout)
		defer cancel()
		if err := s.app.RestartService(ctx, name); err != nil {
			slog.Error("restart failed", "component", "restart", "name", name, "err", err)
		}
		s.PersistState()
	})

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: fmt.Sprintf("restarting %s", name)})
}
