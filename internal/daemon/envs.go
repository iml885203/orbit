package daemon

import (
	"encoding/json"
	"errors"
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
		// Cheap per-request load: envs dir is small (one file per env)
		// and YAML parsing is microseconds. Cache by mtime later if it
		// ever shows up in a profile. Load failures keep PreviewOnly
		// false — surfaces as a graph fetch error at the right time.
		if cfg, err := config.Load(full); err == nil {
			info.PreviewOnly = cfg.PreviewOnly
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

// errEnvPreviewOnly classifies the committed-reload rejection inside the
// env-switch write transaction so the handler maps it to 409, not 500.
var errEnvPreviewOnly = errors.New("env is preview-only")

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

	// Load before stopping anything: a preview-only target must reject
	// pre-flight so the user isn't stranded with the live env stopped
	// and the switch refused.
	probeCfg, err := config.Load(target)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: "load: " + err.Error()})
		return
	}
	if probeCfg.PreviewOnly {
		writeJSON(w, http.StatusConflict, APIResponse{Error: "env " + EnvShortName(target) + " is preview-only and cannot be activated"})
		return
	}

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
	// back this switch (lost update). The early Load above was only
	// preview-rejection validation — the published config must be loaded
	// inside the lock.
	err = s.UpdateConfig(func(tx extension.ConfigTx) error {
		finalCfg, err := tx.Load(target)
		if err != nil {
			return fmt.Errorf("load: %w", err)
		}
		// Re-validate on the value actually being committed: the probe above
		// ran before StopAllServices, and the file could have turned
		// preview-only in between — publishing it would activate an env the
		// probe just promised to reject (TOCTOU).
		if finalCfg.PreviewOnly {
			return errEnvPreviewOnly
		}
		tx.Store(finalCfg)
		tx.SetConfigPath(target)
		// Readers now see the new env, but the orchestrator's service set and
		// DAG are startup snapshots — flag it so status points at a restart
		// (sticky until the daemon actually restarts).
		tx.MarkEngineStale()
		return nil
	})
	if err != nil {
		if errors.Is(err, errEnvPreviewOnly) {
			writeJSON(w, http.StatusConflict, APIResponse{Error: "env " + EnvShortName(target) + " is preview-only and cannot be activated"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	msg := "Env switched to " + EnvShortName(target) + " — API/dashboard now serve it; run `orbit daemon restart` to rebuild the service graph, then `orbit up`."
	if stopped > 0 {
		msg = "Stopped " + strconv.Itoa(stopped) + " services from previous env. " + msg
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
