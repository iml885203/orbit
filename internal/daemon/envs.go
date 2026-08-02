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
	dir := filepath.Dir(current)
	resp := EnvsResponse{Current: current, Envs: []EnvInfo{}}

	for _, name := range ListEnvYamls(dir) {
		full := filepath.Join(dir, name)
		info := EnvInfo{
			Name:    name,
			Path:    full,
			Current: full == current,
		}
		resp.Envs = append(resp.Envs, info)
	}
	sort.Slice(resp.Envs, func(i, j int) bool { return resp.Envs[i].Name < resp.Envs[j].Name })
	resp.Running = s.runningCount()
	writeJSON(w, http.StatusOK, resp)
}

// EnvSwitchRequest is the PUT body for /api/env.
type EnvSwitchRequest struct {
	Env string `json:"env"` // short name (no .yaml) OR absolute path
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
		target = filepath.Join(filepath.Dir(s.ConfigPath()), target)
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
	previousPath := s.ConfigPath()
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
		tx.SetConfigPath(target)
		return nil
	})
	if err != nil {
		if restoreErr := atomicio.WriteFile(CurrentEnvPath(), []byte(previousPath+"\n"), 0644); restoreErr != nil {
			err = fmt.Errorf("%w (restoring previous environment selection: %v)", err, restoreErr)
		}
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	if err := s.restartLauncher(target); err != nil {
		s.UpdateConfig(func(tx extension.ConfigTx) error {
			tx.Store(previousCfg)
			tx.SetConfigPath(previousPath)
			return nil
		})
		if restoreErr := atomicio.WriteFile(CurrentEnvPath(), []byte(previousPath+"\n"), 0644); restoreErr != nil {
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
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: msg})
}

// runningCount returns how many services or containers are currently
// in a non-stopped state. Used to tell the user how disruptive an
// env switch will be.
// runningCount returns how many services or containers are currently
// in a non-stopped state. Used to tell the user how disruptive an
// env switch will be — env switch stops everything (including infra)
// because two envs may share names but differ in spec.
func (s *Server) runningCount() int {
	services := s.app.Orchestrator.GetAllServices()
	n := 0
	for i := range services {
		st := services[i].State.String()
		if st != "stopped" && st != "pending" {
			n++
		}
	}
	return n
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
