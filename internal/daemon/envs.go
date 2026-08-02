package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/iml885203/orbit/atomicio"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/extension"
)

// EnvShortName derives the env short name from a config path —
// basename without extension (e.g. `/.../envs/prod.yaml` → `prod`).
// Owned by the env domain so callers don't reinvent it.
func EnvShortName(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func (s *Server) handleEnvs(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	current := s.ConfigPath()
	context := s.environmentContext()
	listRoot := current
	if context.ManagedSelection != nil {
		listRoot = context.ManagedSelection.Path
	}
	dir := filepath.Dir(listRoot)
	resp := EnvsResponse{Current: current, Envs: []EnvInfo{}, Context: context}

	for _, name := range ListEnvYamls(dir) {
		full := filepath.Join(dir, name)
		info := EnvInfo{
			Name:    name,
			Path:    full,
			Current: context.Kind == "managed" && canonicalEnvironmentPath(full) == context.ConfigPath,
		}
		resp.Envs = append(resp.Envs, info)
	}
	sort.Slice(resp.Envs, func(i, j int) bool { return resp.Envs[i].Name < resp.Envs[j].Name })
	resp.Running = s.runningCount()
	writeJSON(w, http.StatusOK, resp)
}

// EnvSwitchRequest is the PUT body for /api/env.
type EnvSwitchRequest struct {
	Env             string `json:"env"` // short name (no .yaml) OR absolute path
	Confirmed       bool   `json:"confirmed,omitempty"`
	CurrentIdentity string `json:"current_identity,omitempty"`
	TargetIdentity  string `json:"target_identity,omitempty"`
}

func (s *Server) handleEnvSwitch(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodPut) {
		return
	}
	var req EnvSwitchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
		return
	}
	if req.Env == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "env is required"})
		return
	}

	target := req.Env
	if !strings.ContainsAny(target, `/\`) {
		if !strings.HasSuffix(target, ".yaml") {
			target += ".yaml"
		}
		selectionRoot := s.ConfigPath()
		if selection := s.environmentContext().ManagedSelection; selection != nil {
			selectionRoot = selection.Path
		}
		target = filepath.Join(filepath.Dir(selectionRoot), target)
	}
	if _, err := os.Stat(target); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "env not found: " + req.Env})
		return
	}

	_, err := config.Load(target)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: "load: " + err.Error()})
		return
	}
	if s.restartLauncher == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{Error: "environment switching is unavailable in this Orbit build"})
		return
	}
	currentContext := s.environmentContext()
	runningResources := s.runningResourceNames()
	targetIdentity := canonicalEnvironmentPath(target)
	if len(runningResources) > 0 &&
		(!req.Confirmed ||
			req.CurrentIdentity != currentContext.Identity ||
			req.TargetIdentity != targetIdentity) {
		targetContext := EnvironmentContext{
			Kind:        "managed",
			Identity:    targetIdentity,
			DisplayName: EnvShortName(targetIdentity),
			ConfigPath:  targetIdentity,
			Available:   true,
		}
		writeJSON(w, http.StatusConflict, EnvironmentSwitchResponse{
			Error:                "confirmation required before stopping the active environment",
			ConfirmationRequired: true,
			CurrentContext:       &currentContext,
			TargetContext:        &targetContext,
			RunningResources:     runningResources,
		})
		return
	}
	previousPath := s.ConfigPath()
	previousSelection := ReadCurrentEnv()
	previousContext := s.environmentContext()
	previousCfg := s.holder.Load()

	// Stop everything before swapping config — including containers.
	// Two envs may share container names but differ in image / ports /
	// env / volumes; if we kept the old container running, the new cfg
	// would silently disagree with reality. Always reset to a clean
	// slate, even at the cost of cold-starting infra.
	stopped := 0
	if s.app != nil {
		stopped = s.runningCount()
	}
	if stopped > 0 {
		s.app.StopAllServices()
	}

	// Persist the choice (matches `orbit switch` behavior).
	if err := atomicio.WriteFile(CurrentEnvPath(), []byte(target+"\n"), 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	// Writer-serialized publish. The whole reload→Store→baseline flow is
	// one critical section: config.Load reads process env that
	// restartSQLServer mutates, and an unserialized splice could roll
	// back this switch (lost update). The early Load validates the target
	// before services stop; the published config must still be loaded
	// inside the lock.
	err = s.UpdateConfig(func(tx extension.ConfigTx) error {
		finalCfg, err := tx.Load(target)
		if err != nil {
			return fmt.Errorf("load: %w", err)
		}
		tx.Store(finalCfg)
		return nil
	})
	if err != nil {
		if restoreErr := restoreManagedSelection(previousSelection); restoreErr != nil {
			err = fmt.Errorf("%w (restoring previous environment selection: %v)", err, restoreErr)
		}
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}
	s.SetEnvironmentContext(target, "managed")

	if err := s.restartLauncher(target); err != nil {
		s.UpdateConfig(func(tx extension.ConfigTx) error {
			tx.Store(previousCfg)
			return nil
		})
		s.SetEnvironmentContext(previousPath, previousContext.Kind)
		if restoreErr := restoreManagedSelection(previousSelection); restoreErr != nil {
			err = fmt.Errorf("%w (restoring previous environment selection: %v)", err, restoreErr)
		}
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: "schedule daemon restart: " + err.Error()})
		return
	}
	// Until the delayed replacement process takes over, every status surface
	// must admit that the in-memory graph still belongs to the previous env.
	s.engineStale.Store(true)

	msg := "Switching to " + EnvShortName(target) + " — Orbit is restarting to apply the new environment. Start its resources when the dashboard reconnects."
	if stopped > 0 {
		msg = "Stopped " + strconv.Itoa(stopped) + " running items from the previous environment. " + msg
	}
	writeJSON(w, http.StatusOK, EnvironmentSwitchResponse{OK: true, Message: msg})
}

func restoreManagedSelection(path string) error {
	if path == "" {
		err := os.Remove(CurrentEnvPath())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return atomicio.WriteFile(CurrentEnvPath(), []byte(path+"\n"), 0644)
}

func (s *Server) runningCount() int {
	return len(s.runningResourceNames())
}

func (s *Server) runningResourceNames() []string {
	services := s.app.Orchestrator.GetAllServices()
	names := make([]string, 0)
	for i := range services {
		st := services[i].State.String()
		if st != "stopped" && st != "pending" {
			names = append(names, services[i].Name)
		}
	}
	sort.Strings(names)
	return names
}

// CurrentEnvPath is the on-disk env selection pointer (~/.orbit/current).
// Single owner of the path — the daemon writes it on env switch, the
// staleness check and the CLI read it.
func CurrentEnvPath() string {
	return filepath.Join(OrbitDir(), "current")
}

// ReadCurrentEnv returns the selected env path, or "" when no selection
// exists (fresh install, or the file is unreadable).
func ReadCurrentEnv() string {
	data, err := os.ReadFile(CurrentEnvPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
