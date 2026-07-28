package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/iml885203/orbit/atomicio"
)

// DaemonState is the persistent state written to ~/.orbit/state.json.
type DaemonState struct {
	ConfigPath string                       `json:"config_path"`
	StartedAt  time.Time                    `json:"started_at"`
	Processes  map[string]ProcessRecord     `json:"processes"`
	Services   map[string]ServiceStateEntry `json:"services"`
}

// ProcessRecord stores PID/PGID so the daemon can reconnect after crash.
type ProcessRecord struct {
	PID     int    `json:"pid"`
	PGID    int    `json:"pgid"`
	Command string `json:"command"`
	Dir     string `json:"dir"`
}

// ServiceStateEntry is a cached service state for quick status display.
// State matches the string form of engine.ServiceState.
type ServiceStateEntry struct {
	Kind                  string    `json:"kind"`
	State                 string    `json:"state"`
	ContainerStartedAt    time.Time `json:"container_started_at,omitempty"`
	ExternalRestartCount  int       `json:"external_restart_count,omitempty"`
	LastExternalRestart   time.Time `json:"last_external_restart,omitempty"`
	LastExternalStartedAt time.Time `json:"last_external_started_at,omitempty"`
}

// StateFile manages atomic reads/writes of DaemonState.
type StateFile struct {
	path string
	mu   sync.Mutex
}

// NewStateFile creates a state file manager at the given path.
func NewStateFile(path string) *StateFile {
	return &StateFile{path: path}
}

// DefaultStatePath returns ~/.orbit/state.json.
func DefaultStatePath() string {
	return filepath.Join(OrbitDir(), "state.json")
}

// Read loads the state from disk. Returns empty state if file doesn't exist.
func (sf *StateFile) Read() (*DaemonState, error) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	data, err := os.ReadFile(sf.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &DaemonState{
				Processes: make(map[string]ProcessRecord),
				Services:  make(map[string]ServiceStateEntry),
			}, nil
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var state DaemonState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupted — return empty
		return &DaemonState{
			Processes: make(map[string]ProcessRecord),
			Services:  make(map[string]ServiceStateEntry),
		}, nil
	}
	if state.Processes == nil {
		state.Processes = make(map[string]ProcessRecord)
	}
	if state.Services == nil {
		state.Services = make(map[string]ServiceStateEntry)
	}
	return &state, nil
}

// Write atomically writes state to disk.
func (sf *StateFile) Write(state *DaemonState) error {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	return atomicio.WriteFile(sf.path, data, 0644)
}

// Remove deletes the state file.
func (sf *StateFile) Remove() {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	_ = os.Remove(sf.path)
}
