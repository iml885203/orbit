package daemon

import (
	"net/http"
	"sort"

	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/orbit/internal/engine"
)

func (s *Server) handleEnvironmentReconcile(w http.ResponseWriter, r *http.Request) {
	s.environmentTransitionMu.Lock()
	defer s.environmentTransitionMu.Unlock()
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	response, err := s.reconcileEnvironment()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, EnvironmentReconcileResponse{Error: err.Error()})
		return
	}
	response.OK = true
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) reconcileEnvironment() (EnvironmentReconcileResponse, error) {
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
		return s.applyEnvironmentConfig(tx, running, &response)
	})
	if err != nil {
		return response, err
	}
	if response.RestartRequired {
		return response, nil
	}
	if err := s.startReconciledResources(&response); err != nil {
		return response, err
	}
	s.PersistState()
	return response, nil
}

func (s *Server) applyEnvironmentConfig(tx extension.ConfigTx, running map[string]bool, response *EnvironmentReconcileResponse) error {
	cfg, err := tx.Load(s.ConfigPath())
	if err != nil {
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
	s.app.Orchestrator.ApplyConfig(cfg, detached, serviceModes)
	s.SetConfigPath(s.ConfigPath())
	s.engineStale.Store(false)
	for _, name := range plan.Removed {
		if running[name] {
			response.UnavailableResources = append(response.UnavailableResources, name)
		}
	}
	response.RestartedResources = append(response.RestartedResources, plan.Restart...)
	return nil
}

func (s *Server) startReconciledResources(response *EnvironmentReconcileResponse) error {
	if len(response.RestartedResources) == 0 {
		return nil
	}
	affected, err := s.resolveServicesFromRequest(UpRequest{Resources: response.RestartedResources})
	if err != nil {
		return err
	}
	response.AffectedResources = affected
	response.StartedDependencies = AdditionalResourceNames(response.RestartedResources, affected)
	s.app.StartServices(affected)
	return nil
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

// AdditionalResourceNames reports which resources an operation pulled in
// beyond the requested set — the daemon owns this vocabulary, and the CLI's
// env-apply payload reuses it.
func AdditionalResourceNames(requested, affected []string) []string {
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
