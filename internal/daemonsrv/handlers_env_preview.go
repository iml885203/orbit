package daemonsrv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/internal/envsource"
)

func (s *Server) handleEnvironmentPreview(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	s.environmentTransitionMu.RLock()
	defer s.environmentTransitionMu.RUnlock()
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	path := s.ConfigPath()
	if r.URL.Query().Get("config_path") == "" {
		writeJSON(w, http.StatusConflict, APIResponse{Error: "preview requires config_path; select the environment or pass --config"})
		return
	}
	if err := daemon.CheckConfigMatch(r.URL.Query().Get("config_path"), path); err != nil {
		writeJSON(w, http.StatusConflict, APIResponse{Error: err.Error()})
		return
	}
	cfg, err := config.Load(path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: fmt.Sprintf("cannot preview environment changes: %v", err)})
		return
	}
	preview, err := s.previewEnvironmentConfig(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) previewEnvironmentConfig(cfg *config.Config) (daemon.EnvironmentApplyPreview, error) {
	old := s.holder.Load()
	running := runningNameSet(runningResourceNames(s.app.Orchestrator.GetAllServices()))
	pending := s.previewConfigPending()
	plan := engine.ConfigReconcilePlan{}
	if pending {
		plan = engine.PlanConfigReconcile(old, cfg, running, s.settings.GetServiceModes())
	}
	preview := daemon.EnvironmentApplyPreview{
		DaemonRunning: true, ObservedAt: time.Now().UTC(), PID: os.Getpid(), Generation: s.environmentGeneration.Load(), Context: s.previewEnvironmentContext(),
		ChangesPending: pending, RestartRequired: plan.RestartRequired,
		StopResources: append([]string{}, plan.Stop...), RestartResources: append([]string{}, plan.Restart...), RemovedResources: append([]string{}, plan.Removed...),
		StartedDependencies: []string{}, UnavailableResources: []string{}, AffectedResources: []string{},
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return preview, fmt.Errorf("fingerprinting preview configuration: %w", err)
	}
	sum := sha256.Sum256(encoded)
	preview.TargetFingerprint = hex.EncodeToString(sum[:])
	if plan.RestartRequired {
		for name := range running {
			preview.StopResources = append(preview.StopResources, name)
			if cfg.ServiceOrContainerExists(name) {
				preview.RestartResources = append(preview.RestartResources, name)
			} else {
				preview.UnavailableResources = append(preview.UnavailableResources, name)
			}
		}
		for _, name := range runtimePreviewNames(old) {
			if !cfg.ServiceOrContainerExists(name) {
				preview.RemovedResources = append(preview.RemovedResources, name)
			}
		}
	} else {
		for _, name := range plan.Removed {
			if running[name] {
				preview.UnavailableResources = append(preview.UnavailableResources, name)
			}
		}
	}
	affected := make(map[string]bool, len(preview.RestartResources))
	for _, name := range preview.RestartResources {
		affected[name] = true
	}
	envName := EnvShortName(s.ConfigPath())
	registry, err := envsource.Load(envsource.RegistryPath(OrbitDir()))
	if err != nil {
		return preview, fmt.Errorf("reading environment source context: %w", err)
	}
	if _, identity, found := registry.SourceForPath(OrbitDir(), s.ConfigPath()); found {
		envName = identity
	}
	engine.AddDepsWithDetached(cfg, affected, s.settings.GetDetachedEdges(envName))
	for name := range affected {
		preview.AffectedResources = append(preview.AffectedResources, name)
		if !running[name] {
			preview.StartedDependencies = append(preview.StartedDependencies, name)
		}
	}
	for _, names := range [][]string{preview.StopResources, preview.RestartResources, preview.RemovedResources, preview.StartedDependencies, preview.UnavailableResources, preview.AffectedResources} {
		sort.Strings(names)
	}
	return preview, nil
}

func runtimePreviewNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Services)+len(cfg.Containers))
	for name := range cfg.Services {
		names = append(names, name)
	}
	for name := range cfg.Containers {
		if _, exists := cfg.Services[name]; !exists {
			names = append(names, name)
		}
	}
	return names
}

// Preview reads file stamps without refreshing the status cache or pending state.
func (s *Server) previewConfigPending() bool {
	if s.engineStale.Load() {
		return true
	}
	s.pathMu.RLock()
	defer s.pathMu.RUnlock()
	if s.baseline.fromCurrent {
		if current := ReadCurrentEnv(); current != "" && current != s.configPath {
			return true
		}
	}
	for path, base := range s.baseline.files {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) && path != s.configPath && base.missingChecks >= 1 {
				return true
			}
			continue
		}
		if info.ModTime().Equal(base.mtime) && info.Size() == base.size {
			continue
		}
		if hash, _, _, ok := fileStamp(path); ok && hash != base.hash {
			return true
		}
	}
	return false
}

func (s *Server) previewEnvironmentContext() EnvironmentContext {
	s.pathMu.RLock()
	defer s.pathMu.RUnlock()
	kind := s.environmentContextKind
	if kind == "" {
		kind = "explicit"
	}
	identity := s.environmentContextIdentity
	if identity == "" {
		identity = canonicalEnvironmentPath(s.configPath)
	}
	return EnvironmentContext{Kind: kind, Identity: identity, ConfigPath: s.configPath, DisplayName: EnvShortName(s.configPath), Available: configFileAvailable(s.configPath), Running: s.runningCount() > 0}
}
