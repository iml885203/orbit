package daemon

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/orbit/internal/engine"
)

func (s *Server) handleEnvironmentReconcile(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodPost) {
		return
	}

	previouslyRunning := runningResourceNames(s.app.Orchestrator.GetAllServices())
	running := runningNameSet(previouslyRunning)
	response := EnvironmentReconcileResponse{
		PreviouslyRunning:    previouslyRunning,
		RestartedResources:   []string{},
		StartedDependencies:  []string{},
		UnavailableResources: []string{},
		AffectedResources:    []string{},
	}

	err := s.UpdateConfig(func(tx extension.ConfigTx) error {
		cfg, err := tx.Load(s.ConfigPath())
		if err != nil {
			return err
		}
		if cfg.PreviewOnly {
			return fmt.Errorf("preview-only environments cannot be activated")
		}
		if err := s.app.PrepareConfig(cfg); err != nil {
			return err
		}

		serviceModes := s.settings.GetServiceModes()
		plan := engine.PlanConfigReconcile(tx.Current(), cfg, running, serviceModes)
		if plan.RestartRequired {
			response.RestartRequired = true
			return nil
		}
		if err := s.app.StopServicesForConfig(plan.Stop); err != nil {
			return err
		}

		detached := s.settings.GetDetachedEdges(s.currentEnvName())
		s.app.ApplyConfig(cfg, detached, serviceModes)
		s.SetConfigPath(s.ConfigPath())
		s.engineStale.Store(false)
		for _, name := range plan.Removed {
			if running[name] {
				response.UnavailableResources = append(response.UnavailableResources, name)
			}
		}
		response.RestartedResources = append(response.RestartedResources, plan.Restart...)
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, EnvironmentReconcileResponse{Error: err.Error()})
		return
	}
	if response.RestartRequired {
		response.OK = true
		writeJSON(w, http.StatusOK, response)
		return
	}

	if len(response.RestartedResources) > 0 {
		affected, err := s.resolveServicesFromRequest(UpRequest{Resources: response.RestartedResources})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, EnvironmentReconcileResponse{Error: err.Error()})
			return
		}
		response.AffectedResources = affected
		response.StartedDependencies = additionalResourceNames(response.RestartedResources, affected)
		s.app.StartServices(affected)
	}
	s.PersistState()
	response.OK = true
	writeJSON(w, http.StatusOK, response)
}

func runningResourceNames(services []engine.ServiceInfo) []string {
	names := make([]string, 0, len(services))
	for i := range services {
		switch services[i].State {
		case engine.StateStopped, engine.StateStopping:
			continue
		default:
			names = append(names, services[i].Name)
		}
	}
	sort.Strings(names)
	return names
}

func runningNameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

func additionalResourceNames(requested, affected []string) []string {
	requestedSet := make(map[string]bool, len(requested))
	for _, name := range requested {
		requestedSet[name] = true
	}
	additional := make([]string, 0)
	for _, name := range affected {
		if !requestedSet[name] {
			additional = append(additional, name)
		}
	}
	sort.Strings(additional)
	return additional
}
