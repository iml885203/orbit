package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/iml885203/orbit/atomicio"
)

// Settings stores user preferences that persist across daemon restarts.
type Settings struct {
	// WorkspaceRoot is the directory containing the team's repo checkouts.
	WorkspaceRoot string `json:"workspace_root,omitempty"`
	// EnvRepoURL is the git URL `orbit env sync` clones envs from. Only an
	// explicitly user-provided URL is stored here; when empty, resolution
	// falls through to ORBIT_ENV_REPO_URL and the built-in default, so a
	// default change in a new orbit release reaches users who never overrode.
	EnvRepoURL   string            `json:"env_repo_url,omitempty"`
	EnvToggles   map[string]bool   `json:"env_toggles,omitempty"`
	ServiceModes map[string]string `json:"service_modes,omitempty"` // "api": "container"
	UserEnv      map[string]string `json:"user_env,omitempty"`
	ShowHistory  *bool             `json:"show_history,omitempty"`
	// DetachedEdges is a two-level map: env → from → []to.
	// Keys are env names (e.g. "development"); values map a "from" service
	// name to the list of "to" dep names whose edge is suppressed at startup.
	//
	// On-disk format history:
	//   v1 (flat): map[string][]string keyed "<env>/<from>" → []to
	//   v2 (nested, current): map[string]map[string][]string
	// LoadSettings auto-migrates v1 to v2 in-memory; the next Save persists v2.
	DetachedEdges map[string]map[string][]string `json:"detached_edges,omitempty"`

	mu   sync.RWMutex
	path string
	// namespaces are the registered feature-owned key sets (one fresh
	// codec instance per Settings — see RegisterSettingsNamespace). All
	// access happens under mu via the Settings methods.
	namespaces map[string]SettingsNamespaceCodec
	// extensionsRaw carries extension namespaces this binary doesn't own
	// (anything under extensions.* without a registered codec) across
	// load/save.
	// Scope is honest, not absolute: namespaces present at load time are
	// preserved; a namespace written by another process AFTER this one
	// loaded is lost on the next save — the same last-writer-wins
	// snapshot semantics the whole settings file has always had. Guarded
	// by mu.
	extensionsRaw map[string]json.RawMessage
}

// settingsOnDisk is the persisted shape. It differs from the wire shape /api/settings
// marshals: extension-owned keys nest under extensions.<name> on disk but
// stay flat in memory and on the wire.
type settingsOnDisk struct {
	WorkspaceRoot string `json:"workspace_root,omitempty"`

	EnvRepoURL   string                     `json:"env_repo_url,omitempty"`
	EnvToggles   map[string]bool            `json:"env_toggles,omitempty"`
	ServiceModes map[string]string          `json:"service_modes,omitempty"`
	UserEnv      map[string]string          `json:"user_env,omitempty"`
	ShowHistory  *bool                      `json:"show_history,omitempty"`
	Extensions   map[string]json.RawMessage `json:"extensions,omitempty"`
	// RawDetachedEdges holds the raw JSON value for detached_edges so we can
	// try both the nested and legacy flat shapes.
	RawDetachedEdges json.RawMessage `json:"detached_edges,omitempty"`
}

var userEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func ValidateUserEnvName(name string) error {
	if !userEnvNamePattern.MatchString(name) {
		return fmt.Errorf("environment variable name must match [A-Za-z_][A-Za-z0-9_]*")
	}
	return nil
}

func DefaultSettingsPath() string {
	return filepath.Join(OrbitDir(), "settings.json")
}

func LoadSettings(path string) *Settings {
	// Namespace codecs exist even for a fresh (or unreadable) file:
	// Get/Set on feature keys must route the same way regardless of
	// whether anything was on disk yet.
	s := &Settings{path: path, namespaces: newNamespaceCodecs()}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}

	var raw settingsOnDisk
	if err := json.Unmarshal(data, &raw); err != nil {
		slog.Error("failed to parse settings", "component", "settings", "path", path, "err", err)
		return &Settings{path: path, namespaces: newNamespaceCodecs()}
	}

	s.WorkspaceRoot = raw.WorkspaceRoot
	// Registered namespaces hydrate their own keys; everything unregistered
	// is carried opaquely so saves don't drop it.
	for name, codec := range s.namespaces {
		if blob, ok := raw.Extensions[name]; ok {
			codec.Hydrate(blob)
		}
	}
	for name, blob := range raw.Extensions {
		if _, registered := s.namespaces[name]; registered {
			continue
		}
		if s.extensionsRaw == nil {
			s.extensionsRaw = make(map[string]json.RawMessage)
		}
		s.extensionsRaw[name] = blob
	}
	s.EnvRepoURL = raw.EnvRepoURL
	s.EnvToggles = raw.EnvToggles
	s.ServiceModes = raw.ServiceModes
	s.UserEnv = raw.UserEnv
	s.ShowHistory = raw.ShowHistory

	if len(raw.RawDetachedEdges) > 0 {
		s.DetachedEdges = migrateDetachedEdges(raw.RawDetachedEdges, path)
	}

	return s
}

// migrateDetachedEdges attempts to decode the raw JSON as the current nested
// format. If that fails (or the decoded result looks like legacy flat keys
// containing "/"), it migrates from the v1 flat format to v2 nested.
func migrateDetachedEdges(raw json.RawMessage, path string) map[string]map[string][]string {
	// Try nested format first (v2).
	var nested map[string]map[string][]string
	if err := json.Unmarshal(raw, &nested); err == nil {
		// Verify it really is nested (v2) by checking whether any top-level
		// key contains a "/" — that would be the v1 flat form misread as v2
		// (v2 keys are plain env names, never contain "/").
		isFlat := false
		for k := range nested {
			if strings.Contains(k, "/") {
				isFlat = true
				break
			}
		}
		if !isFlat {
			return nested
		}
	}

	// Fall back: try legacy flat format (v1): map[string][]string keyed "<env>/<from>".
	var flat map[string][]string
	if err := json.Unmarshal(raw, &flat); err != nil {
		slog.Error("failed to parse detached_edges (neither v1 nor v2 format)",
			"component", "settings", "path", path)
		return nil
	}

	slog.Info("migrating detached_edges from flat (v1) to nested (v2) format",
		"component", "settings", "path", path)

	out := make(map[string]map[string][]string)
	for key, toList := range flat {
		slash := strings.Index(key, "/")
		if slash < 0 {
			continue // malformed key, skip
		}
		envName := key[:slash]
		from := key[slash+1:]
		if out[envName] == nil {
			out[envName] = make(map[string][]string)
		}
		out[envName][from] = toList
	}
	return out
}

func (s *Settings) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// saveLocked writes settings atomically in the on-disk shape: neutral keys
// flat, extension-owned keys nested under extensions.<name>. The in-memory
// struct (and the /api/settings wire shape that marshals it) stays flat —
// disk is the only place the namespace exists. Caller must hold s.mu.
func (s *Settings) saveLocked() error {
	disk := settingsOnDisk{
		WorkspaceRoot: s.WorkspaceRoot,
		EnvRepoURL:    s.EnvRepoURL,
		EnvToggles:    s.EnvToggles,
		ServiceModes:  s.ServiceModes,
		UserEnv:       s.UserEnv,
		ShowHistory:   s.ShowHistory,
	}
	if len(s.DetachedEdges) > 0 {
		de, err := json.Marshal(s.DetachedEdges)
		if err != nil {
			return err
		}
		disk.RawDetachedEdges = de
	}
	for name, codec := range s.namespaces {
		blob := codec.ToDisk()
		if blob == nil {
			continue
		}
		if disk.Extensions == nil {
			disk.Extensions = make(map[string]json.RawMessage, len(s.namespaces)+len(s.extensionsRaw))
		}
		disk.Extensions[name] = blob
	}
	for name, blob := range s.extensionsRaw {
		if disk.Extensions == nil {
			disk.Extensions = make(map[string]json.RawMessage, len(s.extensionsRaw))
		}
		disk.Extensions[name] = blob
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	return atomicio.WriteFile(s.path, data, 0644)
}

func (s *Settings) Get(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch key {
	case "workspace_root":
		return s.WorkspaceRoot
	case "env_repo_url":
		return s.EnvRepoURL
	}
	for _, codec := range s.namespaces {
		if v, ok := codec.Get(key); ok {
			return v
		}
	}
	return ""
}

func (s *Settings) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch key {
	case "workspace_root":
		s.WorkspaceRoot = value
	case "env_repo_url":
		s.EnvRepoURL = value
	default:
		handled := false
		for _, codec := range s.namespaces {
			ok, err := codec.Set(key, value)
			if err != nil {
				return err
			}
			if ok {
				handled = true
				break
			}
		}
		if !handled {
			// A typo'd key used to be silently dropped (then persisted as a
			// no-op save) — that's how env_repo_url went unpersisted for
			// months. Fail loudly instead.
			return fmt.Errorf("unknown settings key %q", key)
		}
	}
	return s.saveLocked()
}

func (s *Settings) SetShowHistory(value *bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ShowHistory = value
	return s.saveLocked()
}

func (s *Settings) SetUserEnv(key, value string) error {
	if err := ValidateUserEnvName(key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.UserEnv == nil {
		s.UserEnv = make(map[string]string)
	}
	s.UserEnv[key] = value
	return s.saveLocked()
}

func (s *Settings) GetUserEnv(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.UserEnv[key]
}

// ApplyToEnv sets environment variables from settings so config's ${VAR:-default} picks them up.
func (s *Settings) ApplyToEnv() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.WorkspaceRoot != "" {
		_ = os.Setenv("WORKSPACE_ROOT", s.WorkspaceRoot)
	}
	for _, codec := range s.namespaces {
		if codec.EnvExports == nil {
			continue
		}
		for k, v := range codec.EnvExports() {
			_ = os.Setenv(k, v)
		}
	}
	for k, v := range s.UserEnv {
		_ = os.Setenv(k, v)
	}
}

// IsEnvToggleOn returns whether a toggle is on. Falls back to config default.
func (s *Settings) IsEnvToggleOn(key string, configDefault bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.EnvToggles != nil {
		if v, ok := s.EnvToggles[key]; ok {
			return v
		}
	}
	return configDefault
}

// SetEnvToggle sets a toggle state and persists it atomically.
func (s *Settings) SetEnvToggle(key string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.EnvToggles == nil {
		s.EnvToggles = make(map[string]bool)
	}
	s.EnvToggles[key] = on
	return s.saveLocked()
}

// GetEnvToggles returns a copy of all toggle states.
func (s *Settings) GetEnvToggles() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.EnvToggles == nil {
		return nil
	}
	cp := make(map[string]bool, len(s.EnvToggles))
	for k, v := range s.EnvToggles {
		cp[k] = v
	}
	return cp
}

// GetServiceMode returns "dev" (default) or "container" for a service.
func (s *Settings) GetServiceMode(name string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ServiceModes != nil {
		if mode, ok := s.ServiceModes[name]; ok {
			return mode
		}
	}
	return "dev"
}

// SetServiceMode sets the runtime mode for a service and persists it atomically.
func (s *Settings) SetServiceMode(name, mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ServiceModes == nil {
		s.ServiceModes = make(map[string]string)
	}
	if mode == "dev" {
		delete(s.ServiceModes, name)
	} else {
		s.ServiceModes[name] = mode
	}
	return s.saveLocked()
}

// GetServiceModes returns a copy of all service mode overrides.
func (s *Settings) GetServiceModes() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ServiceModes == nil {
		return nil
	}
	cp := make(map[string]string, len(s.ServiceModes))
	for k, v := range s.ServiceModes {
		cp[k] = v
	}
	return cp
}

// WorkspaceRootFromEnv returns the workspace root from the environment.
func WorkspaceRootFromEnv() string {
	return os.Getenv("WORKSPACE_ROOT")
}

// IsEdgeDetached reports whether the dependency from→to is suppressed in env.
func (s *Settings) IsEdgeDetached(env, from, to string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.DetachedEdges[env][from] {
		if t == to {
			return true
		}
	}
	return false
}

// SetEdgeDetached marks the dependency from→to as detached or attached in env,
// then persists atomically. Idempotent (set semantics).
func (s *Settings) SetEdgeDetached(env, from, to string, detached bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DetachedEdges == nil {
		s.DetachedEdges = make(map[string]map[string][]string)
	}
	if s.DetachedEdges[env] == nil {
		s.DetachedEdges[env] = make(map[string][]string)
	}
	list := s.DetachedEdges[env][from]
	idx := -1
	for i, t := range list {
		if t == to {
			idx = i
			break
		}
	}
	if detached {
		if idx == -1 {
			s.DetachedEdges[env][from] = append(list, to)
		}
	} else {
		if idx >= 0 {
			s.DetachedEdges[env][from] = append(list[:idx], list[idx+1:]...)
		}
		if len(s.DetachedEdges[env][from]) == 0 {
			delete(s.DetachedEdges[env], from)
		}
		if len(s.DetachedEdges[env]) == 0 {
			delete(s.DetachedEdges, env)
		}
	}
	return s.saveLocked()
}

// GetDetachedEdges returns a copy of the detached-edge map for env, keyed by
// "from" service name with value = list of detached "to" names. Returns
// empty map when nothing is detached.
func (s *Settings) GetDetachedEdges(env string) map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fromMap := s.DetachedEdges[env]
	out := make(map[string][]string, len(fromMap))
	for from, toList := range fromMap {
		cp := make([]string, len(toList))
		copy(cp, toList)
		out[from] = cp
	}
	return out
}

// MarshalJSON keeps the /api/settings wire shape flat: the neutral
// struct fields plus every registered namespace's keys merged at top
// level — the wire predates the on-disk namespace and must not change.
func (s *Settings) MarshalJSON() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	type wireSettings Settings // method-free alias: avoids marshal recursion
	base, err := json.Marshal((*wireSettings)(s))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	for _, codec := range s.namespaces {
		if codec.WireFlat == nil {
			continue
		}
		for k, v := range codec.WireFlat() {
			m[k] = v
		}
	}
	return json.Marshal(m)
}
