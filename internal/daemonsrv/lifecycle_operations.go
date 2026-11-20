package daemonsrv

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iml885203/orbit/internal/daemon"
)

const lifecycleOperationLimit = 128
const lifecycleActiveLimit = 32

var errLifecycleBusy = errors.New("a mode transition for this resource is already accepted; observe its result before retrying")

var errLifecycleCapacity = errors.New("lifecycle operation capacity reached")
var errLifecycleEnvironmentChanged = errors.New("environment changed; inspect the selected target before retrying")
var errLifecycleEnvironmentPrecondition = errors.New("invalid lifecycle environment precondition")

type lifecycleOperations struct {
	mu      sync.Mutex
	records map[string]daemon.LifecycleOperation
	order   []string
}

func (s *lifecycleOperations) accept(operation daemon.LifecycleOperation) (daemon.LifecycleOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		s.records = make(map[string]daemon.LifecycleOperation)
	}
	if operation.Kind == "mode" {
		for id := range s.records {
			if s.records[id].Kind == "mode" && s.records[id].FinishedAt == nil {
				for _, name := range operation.Resources {
					for _, reserved := range s.records[id].Resources {
						if name == reserved {
							return operation, errLifecycleBusy
						}
					}
				}
			}
		}
	}
	active := 0
	for id := range s.records {
		if s.records[id].FinishedAt == nil {
			active++
		}
	}
	if active >= lifecycleActiveLimit {
		return operation, errLifecycleCapacity
	}
	if len(s.records) >= lifecycleOperationLimit {
		for index, id := range s.order {
			if s.records[id].FinishedAt != nil {
				delete(s.records, id)
				s.order = append(s.order[:index], s.order[index+1:]...)
				break
			}
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return operation, fmt.Errorf("allocate lifecycle operation ID: %w", err)
	}
	operation.ID = hex.EncodeToString(random[:])
	operation.State = "accepted"
	operation.Phase = "queued"
	operation.AcceptedAt = time.Now().UTC()
	operation.Resources = append([]string{}, operation.Resources...)
	sort.Strings(operation.Resources)
	s.records[operation.ID] = operation
	s.order = append(s.order, operation.ID)
	return operation, nil
}

func (s *lifecycleOperations) discardAdmission(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	for index, value := range s.order {
		if value == id {
			s.order = append(s.order[:index], s.order[index+1:]...)
			break
		}
	}
}

func (s *lifecycleOperations) read(id string) (daemon.LifecycleOperation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operation, ok := s.records[id]
	operation.Resources = append([]string{}, operation.Resources...)
	operation.Warnings = append([]string(nil), operation.Warnings...)
	return operation, ok
}

func (s *lifecycleOperations) phase(id, phase string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operation := s.records[id]
	operation.State = "running"
	if cleanup, warnings := lifecycleCleanupResult(phase); cleanup != "" {
		operation.Cleanup = cleanup
		operation.Warnings = warnings
		phase = "sweeping"
	}
	operation.Phase = phase
	if operation.StartedAt == nil {
		now := time.Now().UTC()
		operation.StartedAt = &now
	}
	s.records[id] = operation
}

func lifecycleCleanupResult(phase string) (string, []string) {
	switch phase {
	case "sweep_unavailable":
		return "unavailable", []string{"Orphan container cleanup could not be verified because Docker is unavailable."}
	case "sweep_succeeded":
		return "succeeded", nil
	case "sweep_failed":
		return "failed", nil
	}
	return "", nil
}

func (s *lifecycleOperations) finish(id, code string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operation := s.records[id]
	operation.State = "succeeded"
	operation.Phase = "completed"
	now := time.Now().UTC()
	operation.FinishedAt = &now
	if err != nil {
		operation.State = "failed"
		operation.Error = err.Error()
		operation.Code = code
	}
	s.records[id] = operation
}

func (s *Server) acceptLifecycle(request *http.Request, kind string, names []string, work func(string) error) (daemon.LifecycleOperation, error) {
	s.snapshotMu.RLock()
	if request != nil {
		if err := s.checkLifecycleEnvironment(request); err != nil {
			s.snapshotMu.RUnlock()
			return daemon.LifecycleOperation{}, err
		}
	}
	operation := daemon.LifecycleOperation{Kind: kind, Epoch: s.epoch(), Generation: s.environmentGeneration.Load(), ConfigPath: s.ConfigPath(), Instance: s.instanceName, Resources: names}
	s.snapshotMu.RUnlock()
	operation, err := s.lifecycle.accept(operation)
	if err != nil {
		return operation, err
	}
	s.dispatchLifecycle(operation, work)
	return operation, nil
}

func (s *Server) dispatchLifecycle(operation daemon.LifecycleOperation, work func(string) error) {
	s.startBackground(func() {
		s.environmentTransitionMu.Lock()
		defer s.environmentTransitionMu.Unlock()
		s.configWriteMu.Lock()
		defer s.configWriteMu.Unlock()
		if s.environmentGeneration.Load() != operation.Generation {
			s.lifecycle.finish(operation.ID, "operation_superseded", errors.New("environment changed before accepted operation could start"))
			return
		}
		s.lifecycle.phase(operation.ID, "stopping")
		err := work(operation.ID)
		s.lifecycle.finish(operation.ID, "lifecycle_failed", err)
	})
}

func (s *Server) handleLifecycleOperation(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/operations/")
	operation, ok := s.lifecycle.read(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIResponse{Error: "lifecycle operation is unavailable; preserve the ID and inspect current status before another mutation", Code: "operation_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, operation)
}

func (s *Server) checkLifecycleEnvironment(r *http.Request) error {
	epochText := r.Header.Get(daemon.ExpectedEpochHeader)
	generationText := r.Header.Get(daemon.ExpectedGenerationHeader)
	if epochText == "" && generationText == "" {
		return nil
	}
	epoch, epochErr := strconv.ParseInt(epochText, 10, 64)
	generation, generationErr := strconv.ParseUint(generationText, 10, 64)
	if epochErr != nil || generationErr != nil {
		return errLifecycleEnvironmentPrecondition
	}
	if epoch != s.epoch() || generation != s.environmentGeneration.Load() {
		return errLifecycleEnvironmentChanged
	}
	return nil
}

func (s *Server) beginLifecycleRequest(w http.ResponseWriter, r *http.Request) bool {
	if !s.environmentTransitionMu.TryLock() {
		writeJSON(w, http.StatusConflict, APIResponse{Error: "another environment transition is running; observe its result before retrying", Code: "operation_busy"})
		return false
	}
	if err := s.checkLifecycleEnvironment(r); err != nil {
		s.environmentTransitionMu.Unlock()
		lifecycleAcceptanceError(w, err)
		return false
	}
	return true
}

func lifecycleAcceptanceError(w http.ResponseWriter, err error) {
	code := "lifecycle_acceptance_failed"
	status := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, errLifecycleBusy):
		code = "operation_busy"
		status = http.StatusConflict
	case errors.Is(err, errLifecycleCapacity):
		code = "operation_capacity"
	case errors.Is(err, errLifecycleEnvironmentChanged):
		code = "environment_changed"
		status = http.StatusConflict
	case errors.Is(err, errLifecycleEnvironmentPrecondition):
		code = "invalid_environment_precondition"
		status = http.StatusBadRequest
	}
	writeJSON(w, status, APIResponse{Error: err.Error(), Code: code})
}
