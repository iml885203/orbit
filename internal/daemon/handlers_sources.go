package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/iml885203/orbit/internal/envsource"
)

type sourceMutationRequest struct {
	Action    string `json:"action"`
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	URL       string `json:"url,omitempty"`
	Path      string `json:"path,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Confirmed bool   `json:"confirmed,omitempty"`
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Error: "method not allowed"})
		return
	}
	var request sourceMutationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
		return
	}
	s.environmentTransitionMu.Lock()
	defer s.environmentTransitionMu.Unlock()
	registry, err := loadEnvironmentSourceRegistry()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}
	switch request.Action {
	case "add":
		err = addEnvironmentSource(registry, request)
	case "sync":
		err = syncEnvironmentSource(registry, request.Name)
	case "sync_all":
		err = syncAllEnvironmentSources(registry)
	case "remove":
		err = s.removeEnvironmentSource(registry, request)
	case "ack_migration":
		err = envsource.AcknowledgeLegacyMigrationNotice(OrbitDir())
	default:
		err = fmt.Errorf("unknown source action %q", request.Action)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: "environment source " + request.Action + " complete"})
}

func addEnvironmentSource(registry *envsource.Registry, request sourceMutationRequest) error {
	if _, err := registry.Get(request.Name); !errors.Is(err, envsource.ErrNotFound) {
		if err != nil {
			return err
		}
		return fmt.Errorf("environment source %q already exists", request.Name)
	}
	source := envsource.Source{Name: request.Name, Type: request.Type, URL: request.URL, Path: request.Path, Ref: request.Ref, Workspace: request.Workspace}
	if source.Type == envsource.TypeLocal {
		normalized, err := envsource.ValidateLocalSource(source.Path)
		if err != nil {
			return err
		}
		source.Path = normalized
	}
	if source.Workspace != "" {
		normalized, err := envsource.NormalizeExistingDirectory(source.Workspace)
		if err != nil {
			return err
		}
		source.Workspace = normalized
	}
	refreshed, _, err := envsource.Refresh(registry, source, OrbitDir(), false, false)
	if err != nil {
		return err
	}
	source = refreshed
	if err := registry.Add(source); err != nil {
		_ = os.RemoveAll(envsource.SourceDir(OrbitDir(), source.Name))
		return err
	}
	return nil
}

func syncAllEnvironmentSources(registry *envsource.Registry) error {
	var failures []error
	for _, source := range registry.List() {
		if err := syncEnvironmentSource(registry, source.Name); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", source.Name, err))
		}
	}
	return errors.Join(failures...)
}

func syncEnvironmentSource(registry *envsource.Registry, name string) error {
	source, err := registry.Get(name)
	if err != nil {
		return err
	}
	_, _, err = envsource.Refresh(registry, source, OrbitDir(), false, true)
	return err
}

func (s *Server) removeEnvironmentSource(registry *envsource.Registry, request sourceMutationRequest) error {
	source, err := registry.Get(request.Name)
	if err != nil {
		return err
	}
	context := s.environmentContext()
	if context.Kind == "managed" && strings.HasPrefix(context.Identity, source.Name+"/") && context.Running {
		return fmt.Errorf("source owns running environment %s; switch or stop it first", context.Identity)
	}
	selected := ReadCurrentEnv()
	selectedOwned := envsource.ContainsPath(OrbitDir(), source.Name, selected)
	if selectedOwned && !request.Confirmed {
		return errors.New("confirmation required to clear the selected environment")
	}
	_, err = envsource.RemoveOwned(registry, OrbitDir(), source.Name, CurrentEnvPath(), selected)
	return err
}
