// Package autoupdate owns installation-wide update discovery and transaction state.
package autoupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/iml885203/orbit/atomicio"
)

const (
	EnvLaunchPath   = "ORBIT_INSTALLATION_LAUNCH_PATH"
	PolicyAutomatic = "automatic"
	PolicyOff       = "off"
	OwnerDirect     = "direct"
	OwnerHomebrew   = "homebrew"
	OwnerScoop      = "scoop"
)

func LaunchPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv(EnvLaunchPath)); value != "" {
		return filepath.Abs(value)
	}
	return os.Executable()
}

type State struct {
	InstallationID  string             `json:"installation_id"`
	LaunchPath      string             `json:"launch_path"`
	Owner           string             `json:"owner"`
	Policy          string             `json:"policy"`
	DisclosureShown bool               `json:"disclosure_shown"`
	CurrentVersion  string             `json:"current_version,omitempty"`
	TargetVersion   string             `json:"target_version,omitempty"`
	Phase           string             `json:"phase,omitempty"`
	ApplyEligible   bool               `json:"apply_eligible"`
	DeferReason     string             `json:"defer_reason,omitempty"`
	StagedBinary    string             `json:"staged_binary,omitempty"`
	LastCheckedAt   *time.Time         `json:"last_checked_at,omitempty"`
	NextCheckAt     *time.Time         `json:"next_check_at,omitempty"`
	LastError       string             `json:"last_error,omitempty"`
	CheckFailures   int                `json:"check_failures,omitempty"`
	Transaction     *Transaction       `json:"transaction,omitempty"`
	Runtimes        map[string]Runtime `json:"runtimes,omitempty"`
}

type Runtime struct {
	Identity       string    `json:"identity"`
	Home           string    `json:"home"`
	SocketPath     string    `json:"socket_path"`
	Instance       string    `json:"instance,omitempty"`
	Executable     string    `json:"executable"`
	Build          string    `json:"build"`
	PID            int       `json:"pid,omitempty"`
	ProcessStarted string    `json:"process_started,omitempty"`
	LastSeenAt     time.Time `json:"last_seen_at"`
}

type Transaction struct {
	ID              string                    `json:"id"`
	Operation       string                    `json:"operation"`
	Phase           string                    `json:"phase"`
	TargetVersion   string                    `json:"target_version,omitempty"`
	StartedAt       time.Time                 `json:"started_at"`
	FinishedAt      *time.Time                `json:"finished_at,omitempty"`
	Error           string                    `json:"error,omitempty"`
	WorkerPID       int                       `json:"worker_pid,omitempty"`
	HeartbeatAt     *time.Time                `json:"heartbeat_at,omitempty"`
	RuntimeOutcomes map[string]RuntimeOutcome `json:"runtime_outcomes,omitempty"`
}

func SetTransactionWorker(launchPath, transactionID string, pid int) (State, error) {
	return Update(launchPath, func(state *State) error {
		if state.Transaction == nil || state.Transaction.FinishedAt != nil || state.Transaction.ID != transactionID {
			return errors.New("no active update transaction")
		}
		now := time.Now().UTC()
		state.Transaction.WorkerPID = pid
		state.Transaction.HeartbeatAt = &now
		return nil
	})
}

func HeartbeatTransaction(launchPath, transactionID string) error {
	_, err := Update(launchPath, func(state *State) error {
		if state.Transaction == nil || state.Transaction.ID != transactionID || state.Transaction.FinishedAt != nil {
			return errors.New("update transaction is no longer active")
		}
		now := time.Now().UTC()
		state.Transaction.HeartbeatAt = &now
		return nil
	})
	return err
}

type RuntimeOutcome struct {
	PreviouslyRunning []string `json:"previously_running,omitempty"`
	RestoredResources []string `json:"restored_resources,omitempty"`
	Phase             string   `json:"phase"`
	Error             string   `json:"error,omitempty"`
}

type Summary struct {
	InstallationID  string                    `json:"installation_id"`
	Owner           string                    `json:"owner"`
	Policy          string                    `json:"policy"`
	DisclosureShown bool                      `json:"disclosure_shown"`
	CurrentVersion  string                    `json:"current_version,omitempty"`
	TargetVersion   string                    `json:"target_version,omitempty"`
	Phase           string                    `json:"phase,omitempty"`
	ApplyEligible   bool                      `json:"apply_eligible"`
	DeferReason     string                    `json:"defer_reason,omitempty"`
	LastError       string                    `json:"last_error,omitempty"`
	Transaction     *Transaction              `json:"transaction,omitempty"`
	RuntimeOutcomes map[string]RuntimeOutcome `json:"runtime_outcomes,omitempty"`
}

func (s State) Summary() Summary {
	outcomes := map[string]RuntimeOutcome(nil)
	if s.Transaction != nil {
		outcomes = s.Transaction.RuntimeOutcomes
	}
	return Summary{
		InstallationID: s.InstallationID, Owner: s.Owner, Policy: s.Policy,
		DisclosureShown: s.DisclosureShown, CurrentVersion: s.CurrentVersion,
		TargetVersion: s.TargetVersion, Phase: s.Phase, ApplyEligible: s.ApplyEligible,
		DeferReason: s.DeferReason, LastError: s.LastError,
		Transaction: s.Transaction, RuntimeOutcomes: outcomes,
	}
}

func SetPolicy(launchPath, policy string) (State, error) {
	if policy != PolicyAutomatic && policy != PolicyOff {
		return State{}, fmt.Errorf("invalid automatic update policy %q", policy)
	}
	return Update(launchPath, func(state *State) error {
		state.Policy = policy
		return nil
	})
}

func RegisterRuntime(launchPath string, runtime Runtime) (State, error) {
	return Update(launchPath, func(state *State) error {
		runtime.LastSeenAt = time.Now().UTC()
		state.Runtimes[runtime.Identity] = runtime
		return nil
	})
}

func UnregisterRuntime(launchPath, identity string) (State, error) {
	return Update(launchPath, func(state *State) error {
		delete(state.Runtimes, identity)
		return nil
	})
}

func BeginTransaction(launchPath, operation, target string) (State, error) {
	return Update(launchPath, func(state *State) error {
		if state.Transaction != nil && state.Transaction.FinishedAt == nil {
			return fmt.Errorf("update transaction %s is already %s", state.Transaction.ID, state.Transaction.Phase)
		}
		now := time.Now().UTC()
		state.Phase = "applying"
		state.TargetVersion = target
		state.Transaction = &Transaction{
			ID: fmt.Sprintf("%d-%d", now.UnixNano(), os.Getpid()), Operation: operation,
			Phase: "applying", TargetVersion: target, StartedAt: now,
			RuntimeOutcomes: map[string]RuntimeOutcome{},
		}
		return nil
	})
}

func RecordRuntimeOutcome(launchPath, transactionID, identity string, outcome RuntimeOutcome) (State, error) {
	return Update(launchPath, func(state *State) error {
		if state.Transaction == nil || state.Transaction.FinishedAt != nil || state.Transaction.ID != transactionID {
			return errors.New("no active update transaction")
		}
		state.Transaction.RuntimeOutcomes[identity] = outcome
		return nil
	})
}

func FinishTransaction(launchPath, transactionID, phase string, transactionErr error) (State, error) {
	return Update(launchPath, func(state *State) error {
		if state.Transaction == nil || state.Transaction.ID != transactionID {
			return errors.New("no update transaction")
		}
		now := time.Now().UTC()
		state.Transaction.Phase = phase
		state.Transaction.FinishedAt = &now
		state.Phase = phase
		state.LastError = ""
		if transactionErr != nil {
			state.Transaction.Error = transactionErr.Error()
			state.LastError = transactionErr.Error()
		}
		if phase == "succeeded" || phase == "partial" {
			state.CurrentVersion = state.TargetVersion
			state.TargetVersion = ""
			state.StagedBinary = ""
			state.ApplyEligible = false
			state.DeferReason = ""
		}
		return nil
	})
}

func InstallationArtifacts(launchPath string) ([]string, error) {
	id, _, err := InstallationID(launchPath)
	if err != nil {
		return nil, err
	}
	dir, err := GlobalDir()
	if err != nil {
		return nil, err
	}
	return []string{filepath.Join(dir, "updates", id), filepath.Join(dir, "helpers", id)}, nil
}

func RemoveInstallation(launchPath string) error {
	id, _, err := InstallationID(launchPath)
	if err != nil {
		return err
	}
	if err := withRegistryLock(func(reg *registry) error {
		delete(reg.Installations, id)
		return nil
	}); err != nil {
		return err
	}
	artifacts, err := InstallationArtifacts(launchPath)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if err := os.RemoveAll(artifact); err != nil {
			return fmt.Errorf("remove update artifact %s: %w", artifact, err)
		}
	}
	return nil
}

type registry struct {
	Installations map[string]State `json:"installations"`
}

func GlobalDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("ORBIT_UPDATE_HOME")); override != "" {
		return override, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(root, "orbit"), nil
}

func InstallationID(launchPath string) (string, string, error) {
	abs, err := filepath.Abs(launchPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve Orbit launch path: %w", err)
	}
	stable := filepath.Clean(abs)
	lower := strings.ToLower(filepath.ToSlash(stable))
	if marker := strings.Index(lower, "/cellar/orbit/"); marker >= 0 {
		stable = filepath.FromSlash(lower[:marker] + "/opt/orbit/bin/orbit")
	}
	if runtime.GOOS == "windows" {
		stable = strings.ToLower(stable)
	}
	sum := sha256.Sum256([]byte(stable))
	return "orbit-" + hex.EncodeToString(sum[:8]), stable, nil
}

func Owner(launchPath string) string {
	ownerPath := launchPath
	if resolved, err := filepath.EvalSymlinks(launchPath); err == nil {
		ownerPath = resolved
	}
	normalized := strings.ToLower(filepath.ToSlash(ownerPath))
	if strings.Contains(normalized, "/cellar/orbit/") || strings.Contains(normalized, "/opt/homebrew/opt/orbit/") {
		return OwnerHomebrew
	}
	if strings.Contains(normalized, "/apps/orbit/") {
		return OwnerScoop
	}
	return OwnerDirect
}

func Load(launchPath string) (State, error) {
	id, stable, err := InstallationID(launchPath)
	if err != nil {
		return State{}, err
	}
	reg, err := loadRegistry()
	if err != nil {
		return State{}, err
	}
	state, ok := reg.Installations[id]
	if !ok {
		state = State{InstallationID: id, LaunchPath: stable, Owner: Owner(launchPath), Policy: PolicyAutomatic, Runtimes: map[string]Runtime{}}
	}
	if state.Policy == "" {
		state.Policy = PolicyAutomatic
	}
	if state.Runtimes == nil {
		state.Runtimes = map[string]Runtime{}
	}
	return state, nil
}

func Save(state State) error {
	if state.InstallationID == "" {
		return errors.New("update state has no installation ID")
	}
	return withRegistryLock(func(reg *registry) error {
		reg.Installations[state.InstallationID] = state
		return nil
	})
}

func Update(launchPath string, change func(*State) error) (State, error) {
	id, stable, err := InstallationID(launchPath)
	if err != nil {
		return State{}, err
	}
	var result State
	err = withRegistryLock(func(reg *registry) error {
		state, ok := reg.Installations[id]
		if !ok {
			state = State{InstallationID: id, LaunchPath: stable, Owner: Owner(launchPath), Policy: PolicyAutomatic, Runtimes: map[string]Runtime{}}
		}
		if state.Runtimes == nil {
			state.Runtimes = map[string]Runtime{}
		}
		if err := change(&state); err != nil {
			return err
		}
		reg.Installations[id] = state
		result = state
		return nil
	})
	return result, err
}

func loadRegistry() (registry, error) {
	dir, err := GlobalDir()
	if err != nil {
		return registry{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "update-registry.json"))
	if errors.Is(err, os.ErrNotExist) {
		return registry{Installations: map[string]State{}}, nil
	}
	if err != nil {
		return registry{}, fmt.Errorf("read update registry: %w", err)
	}
	var reg registry
	if err := json.Unmarshal(data, &reg); err != nil {
		path := filepath.Join(dir, "update-registry.json")
		quarantine := fmt.Sprintf("%s.corrupt-%d", path, time.Now().UnixNano())
		if renameErr := os.Rename(path, quarantine); renameErr != nil {
			return registry{}, fmt.Errorf("quarantine corrupt update registry: %w", renameErr)
		}
		return registry{Installations: map[string]State{}}, nil
	}
	if reg.Installations == nil {
		reg.Installations = map[string]State{}
	}
	return reg, nil
}

func withRegistryLock(change func(*registry) error) error {
	dir, err := GlobalDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create update state directory: %w", err)
	}
	unlock, err := acquireLock(filepath.Join(dir, "update-registry.lock"), 5*time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if err := change(&reg); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode update registry: %w", err)
	}
	if err := atomicio.WriteFile(filepath.Join(dir, "update-registry.json"), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write update registry: %w", err)
	}
	return nil
}

func acquireLock(path string, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	for {
		err := os.Mkdir(path, 0o700)
		if err == nil {
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create update registry lock: %w", err)
		}
		info, statErr := os.Stat(path)
		if statErr == nil && time.Since(info.ModTime()) > time.Minute {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for update registry lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
