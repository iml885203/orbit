package bigquery

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gofrs/flock"
)

type Receipt struct {
	ProfileName       string            `json:"profile_name"`
	Version           string            `json:"version"`
	StoreID           string            `json:"store_id"`
	Scope             string            `json:"scope"`
	RunKey            string            `json:"run_key"`
	Generation        string            `json:"generation"`
	ConfigHash        string            `json:"config_hash"`
	FixtureHash       string            `json:"fixture_hash"`
	Target            Profile           `json:"target"`
	DatasetID         string            `json:"dataset_id"`
	TableGenerations  map[string]int64  `json:"table_generations"`
	Jobs              map[string]string `json:"jobs"`
	JobStates         map[string]string `json:"job_states"`
	RuntimeGeneration string            `json:"runtime_generation"`
	Instance          string            `json:"instance"`
	ObservedAt        string            `json:"observed_at,omitempty"`
	CreationTime      int64             `json:"creation_time"`
	Phase             string            `json:"phase"`
	LastError         string            `json:"last_error,omitempty"`
	Warnings          []string          `json:"warnings,omitempty"`
	DataReady         bool              `json:"sandbox_data_ready"`
	AppRuntime        string            `json:"app_runtime"`
	AppConfig         map[string]string `json:"app_config"`
}

type operationError struct {
	Code  string
	Cause error
}

func (e *operationError) Error() string { return fmt.Sprintf("%s: %v", e.Code, e.Cause) }
func (e *operationError) CLIJSONRetryable() bool {
	switch e.Code {
	case "operation_busy", "job_pending", "load_submit_unknown", "job_state_unknown", "dataset_create_unknown":
		return true
	default:
		return false
	}
}
func (e *operationError) ErrorCode() string { return e.Code }
func (e *operationError) CLIJSONHint() string {
	switch e.Code {
	case "receipt_invalid", "state_owner_invalid", "runtime_generation_invalid":
		return "Preserve the local evidence. Do not edit receipts to bypass checks; follow docs/bigquery.md manual disposition."
	case "foreign_generation_mutation_blocked", "cleanup_generation_unknown", "cleanup_job_unknown", "cleanup_job_pending", "cleanup_unknown", "dataset_generation_changed", "ownership_mismatch":
		return "Automatic deletion is blocked. Stop app/external jobs, preserve evidence and follow docs/bigquery.md recovery/manual disposition."
	case "cleanup_requires_confirmation_and_quiescence":
		return "Only after authorizing recursive cloud deletion, stopping app/external jobs and restricting writers, pass --yes --app-stopped --restricted-writers."
	case "sandbox_config_conflict", "new_run_key_required":
		return "Account for or dispose of the original dataset before selecting a fresh run key; never change local evidence to bypass the conflict."
	default:
		return "Inspect the canonical sandbox record and retry the same run key after resolving the condition. See docs/bigquery.md. Cloud access and application readiness are separate checks."
	}
}

func (e *operationError) Unwrap() error { return e.Cause }

func failure(code string, cause error) error {
	if cause == nil {
		cause = errors.New(code)
	}
	return &operationError{Code: code, Cause: cause}
}

func errorCode(err error) string {
	var fault *operationError
	if errors.As(err, &fault) {
		return fault.Code
	}
	return "unexpected"
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func nonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

type receiptStore struct {
	dir string
	id  string
}

func openStore(dir string) (*receiptStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	lock := flock.New(filepath.Join(dir, "store.lock"))
	if ok, err := lock.TryLock(); err != nil || !ok {
		return nil, failure("operation_busy", err)
	}
	defer func() { _ = lock.Unlock() }()
	path := filepath.Join(dir, "owner.json")
	var owner struct{ ID string }
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		owner.ID = nonce()
		err = saveJSON(path, owner)
	} else if err == nil {
		err = json.Unmarshal(data, &owner)
	}
	if err != nil {
		return nil, fmt.Errorf("load state owner: %w", err)
	}
	if decoded, decodeErr := hex.DecodeString(owner.ID); decodeErr != nil || len(decoded) != 16 {
		return nil, failure("state_owner_invalid", nil)
	}
	return &receiptStore{dir: dir, id: owner.ID}, nil
}

func (s *receiptStore) path(scope string) string {
	return filepath.Join(s.dir, scope+".json")
}

func (s *receiptStore) acquire(scope string) (func(), error) {
	if decoded, err := hex.DecodeString(scope); err != nil || len(decoded) != 32 {
		return nil, failure("receipt_invalid", nil)
	}
	lock := flock.New(filepath.Join(s.dir, scope+".lock"))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("lock operation: %w", err)
	}
	if !ok {
		return nil, failure("operation_busy", nil)
	}
	return func() { _ = lock.Unlock() }, nil
}

func (s *receiptStore) read(scope string) (*Receipt, error) {
	data, err := os.ReadFile(s.path(scope))
	if err != nil {
		return nil, err
	}
	var rec Receipt
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("read receipt: %w", err)
	}
	if err := validateReceipt(&rec, scope); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *receiptStore) save(rec *Receipt) error {
	return saveJSON(s.path(rec.Scope), rec)
}

func saveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".receipt-*")
	if err != nil {
		return fmt.Errorf("create receipt staging file: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("write receipt: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync receipt: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close receipt: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("publish receipt: %w", err)
	}
	if runtime.GOOS != "windows" {
		dir, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		defer func() { _ = dir.Close() }()
		if err := dir.Sync(); err != nil {
			return err
		}
	}
	return nil
}

func validateReceipt(rec *Receipt, scope string) error {
	validHex := func(value string, size int) bool {
		decoded, err := hex.DecodeString(value)
		return err == nil && len(decoded) == size
	}
	if rec.Version != "orbit.bq.receipt.v1" || rec.Scope != scope || !validHex(scope, 32) || !validHex(rec.Generation, 16) || !validHex(rec.StoreID, 16) || !validHex(rec.RuntimeGeneration, 16) || !validHex(rec.ConfigHash, 32) || !validHex(rec.FixtureHash, 32) || rec.RunKey == "" || scopeFor(rec.Target, rec.RunKey) != scope || rec.DatasetID != "orbit_test_"+rec.Generation || rec.Target.Mode != "sandbox" || rec.ProfileName != rec.Target.ID || rec.Jobs == nil || rec.JobStates == nil || rec.TableGenerations == nil {
		return failure("receipt_invalid", nil)
	}
	c := BQConfig{Identity: "receipt", Profiles: map[string]Profile{rec.Target.ID: rec.Target}}
	if err := c.validate(); err != nil {
		return failure("receipt_invalid", err)
	}
	config, _ := json.Marshal(rec.Target)
	if digest(config) != rec.ConfigHash || len(rec.Jobs) != len(rec.Target.Tables) || len(rec.JobStates) != len(rec.Jobs) {
		return failure("receipt_invalid", nil)
	}
	switch rec.Phase {
	case "intent", "loading", "data_ready", "cleanup_intent", "cleaned":
	default:
		return failure("receipt_invalid", nil)
	}
	for table := range rec.Target.Tables {
		if rec.Jobs[table] != "orbit_load_"+rec.Generation+"_"+digest([]byte(table))[:16] {
			return failure("receipt_invalid", nil)
		}
		if rec.JobStates[table] != "planned" && rec.TableGenerations[table] <= 0 {
			return failure("receipt_invalid", nil)
		}
		switch rec.JobStates[table] {
		case "planned", "submission_intent", "terminal", "schema_only":
		default:
			return failure("receipt_invalid", nil)
		}
	}
	for table, generation := range rec.TableGenerations {
		if _, ok := rec.Jobs[table]; !ok || generation <= 0 {
			return failure("receipt_invalid", nil)
		}
	}
	for table := range rec.JobStates {
		if _, ok := rec.Jobs[table]; !ok {
			return failure("receipt_invalid", nil)
		}
	}
	return nil
}
