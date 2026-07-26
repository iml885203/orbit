package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/engine"
)

// handleHealth responds with {OK:true} for liveness probes.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, APIResponse{OK: true})
}

// handleStatus returns the current status of every configured service
// and container, merging tracked orchestrator state with config-only
// entries so stopped services still appear.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	statuses := s.computeStatuses(s.holder.Load())
	stale, staleReason := s.configStale()
	resp := StatusResponse{
		Epoch:             s.epoch(),
		Services:          statuses,
		ConfigStale:       stale,
		ConfigStaleReason: staleReason,
	}
	writeJSON(w, http.StatusOK, resp)
}

// computeStatuses assembles the current ServiceStatus for every service and
// container tracked by the orchestrator, preserving GetAllServices order.
// computeStatuses assembles ServiceStatus views against the caller's config
// snapshot — the caller owns the one-Load-per-operation boundary, so a
// handler that also builds other views (graph, resources) renders
// everything from a single generation.
func (s *Server) computeStatuses(cfg *config.Config) []ServiceStatus {
	services := s.app.Orchestrator.GetAllServices()
	out := make([]ServiceStatus, 0, len(services))
	for i := range services {
		svc := &services[i]
		ports := getServicePorts(cfg, svc.Name, svc.Kind)
		url := ""
		if svcCfg, ok := cfg.Services[svc.Name]; ok {
			url = svcCfg.URL
		}
		startupTime := ""
		uptime := ""
		if !svc.StartedAt.IsZero() && !svc.HealthyAt.IsZero() {
			startupTime = formatDuration(svc.HealthyAt.Sub(svc.StartedAt))
		}
		if !svc.HealthyAt.IsZero() && svc.State == engine.StateHealthy {
			uptime = formatDuration(time.Since(svc.HealthyAt))
		}
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
		out = append(out, ServiceStatus{
			Name:           svc.Name,
			Kind:           ServiceKind(svc.Kind),
			State:          svc.State.String(),
			StateReason:    svc.StateReason,
			RestartCount:   svc.RestartCount,
			Ports:          ports,
			URL:            url,
			Image:          image,
			StartupTime:    startupTime,
			Uptime:         uptime,
			Sidecars:       sidecars,
			Mode:           mode,
			HealthProgress: hp,
		})
	}
	return out
}

func (s *Server) handleUp(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	if s.rejectIfPreview(w) {
		return
	}

	var req UpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid request body"})
		return
	}

	if req.InfraOnly {
		var containerNames []string
		for name := range s.holder.Load().Containers {
			containerNames = append(containerNames, name)
		}
		s.app.StartServices(containerNames)
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: fmt.Sprintf("starting %d containers", len(containerNames))})
		return
	}

	names, err := s.resolveServicesFromRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: err.Error()})
		return
	}

	s.app.StartServices(names)
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: fmt.Sprintf("starting %d services", len(names))})
}

func (s *Server) resolveServicesFromRequest(req UpRequest) ([]string, error) {
	if len(req.Services) > 0 {
		return s.resolveExplicitServices(req.Services)
	}
	detached := s.settings.GetDetachedEdges(s.currentEnvName())
	enabled := engine.FilterEnabledServicesWithDetached(s.holder.Load(), req.Groups, detached)
	names := make([]string, 0, len(enabled))
	for name := range enabled {
		names = append(names, name)
	}
	return names, nil
}

func (s *Server) resolveExplicitServices(services []string) ([]string, error) {
	detached := s.settings.GetDetachedEdges(s.currentEnvName())
	cfg := s.holder.Load()
	for _, name := range services {
		if !cfg.ServiceOrContainerExists(name) {
			return nil, fmt.Errorf("unknown service: %s", name)
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
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	if s.rejectIfPreview(w) {
		return
	}

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
		go func() {
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
		}()
		return
	}

	// Stop all services and containers in parallel through the canonical
	// StopService lifecycle. Status pollers (orbit down's progress
	// renderer) see real stopping → stopped transitions instead of a
	// sudden state jump at the end.
	go func() {
		s.app.StopAllServices()
		s.PersistState()
		s.stateFile.Remove()
	}()
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: "stopping all services and containers"})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	if s.rejectIfPreview(w) {
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/stop/")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "service name required"})
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.holder.Load().Settings.ShutdownTimeout)
		defer cancel()
		if err := s.app.StopService(ctx, name); err != nil {
			slog.Error("stop failed", "component", "stop", "name", name, "err", err)
		}
		s.PersistState()
	}()

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: fmt.Sprintf("stopping %s", name)})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodPost) {
		return
	}
	if s.rejectIfPreview(w) {
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/restart/")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "service name required"})
		return
	}

	go func() {
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
	}()

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: fmt.Sprintf("restarting %s", name)})
}
