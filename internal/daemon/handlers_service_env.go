package daemon

import (
	"net/http"
	"strings"

	"github.com/iml885203/orbit/internal/env"
)

// ServiceEnvResponse is the response for GET /api/service-env/{name}.
type ServiceEnvResponse struct {
	Service string        `json:"service"`
	Env     []EnvVarEntry `json:"env"`
}

// EnvVarEntry is one env var that will be (or is being) injected into the service process.
type EnvVarEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"` // "explicit" | "toggle" | "dependency"
	// Dependency is the container name this var came from, when Source == "dependency".
	// Empty otherwise.
	Dependency string `json:"dependency,omitempty"`
}

func (s *Server) handleServiceEnv(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/service-env/")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "service name required"})
		return
	}

	toggleStates := s.settings.GetEnvToggles()
	cfg := s.holder.Load()
	svc, ok := cfg.Services[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, APIResponse{Error: "service not found"})
		return
	}
	annotated := env.AnnotatedEnv(svc, cfg.Containers, toggleStates)

	entries := make([]EnvVarEntry, len(annotated))
	for i, e := range annotated {
		entries[i] = EnvVarEntry{
			Key:        e.Key,
			Value:      e.Value,
			Source:     e.Source,
			Dependency: e.Dependency,
		}
	}

	writeJSON(w, http.StatusOK, ServiceEnvResponse{Service: name, Env: entries})
}
