package container

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/iml885203/orbit/config"
)

// SeedState tracks which seed files have been executed.
type SeedState struct {
	Executed map[string]SeedRecord `json:"executed"`
}

// SeedRecord tracks a single seed file execution.
type SeedRecord struct {
	Hash  string    `json:"hash"`
	RanAt time.Time `json:"ran_at"`
}

// SeedResult describes the outcome of running a single seed file.
type SeedResult struct {
	File    string
	Status  string // "executed", "skipped", "changed", "failed"
	Message string
}

var seedStatePath = filepath.Join(os.Getenv("HOME"), ".orbit", "seed-state.json")

func loadSeedState() *SeedState {
	data, err := os.ReadFile(seedStatePath)
	if err != nil {
		return &SeedState{Executed: make(map[string]SeedRecord)}
	}
	var state SeedState
	if err := json.Unmarshal(data, &state); err != nil {
		return &SeedState{Executed: make(map[string]SeedRecord)}
	}
	if state.Executed == nil {
		state.Executed = make(map[string]SeedRecord)
	}
	return &state
}

func saveSeedState(state *SeedState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(seedStatePath, data, 0644)
}

func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// RunSeed executes seed files for a container. Returns results for each file.
func RunSeed(name string, cfg *config.Container, force bool) []SeedResult {
	if cfg.Seed == nil || len(cfg.Seed.Files) == 0 {
		return nil
	}

	state := loadSeedState()
	var results []SeedResult

	for _, file := range cfg.Seed.Files {
		key := name + ":" + file

		hash, err := fileHash(file)
		if err != nil {
			results = append(results, SeedResult{File: file, Status: "failed", Message: err.Error()})
			continue
		}

		// Check if already executed
		if !force {
			if prev, ok := state.Executed[key]; ok {
				if prev.Hash == hash {
					results = append(results, SeedResult{File: file, Status: "skipped", Message: "already executed"})
					continue
				}
				results = append(results, SeedResult{File: file, Status: "changed", Message: "file changed since last run, use --force to re-run"})
				continue
			}
		}

		var execErr error
		containerName := ContainerName(os.Getenv("ORBIT_NAMESPACE"), name)
		switch cfg.Seed.Type {
		case "sqlserver":
			execErr = seedSQLServer(
				containerName,
				cfg.Seed.Username,
				cfg.Seed.PasswordEnv,
				file,
			)
		case "mongo":
			db := cfg.Seed.Database
			execErr = seedMongoDB(containerName, db, file)
		default:
			results = append(results, SeedResult{File: file, Status: "failed", Message: "unsupported container type for seeding"})
			continue
		}

		if execErr != nil {
			results = append(results, SeedResult{File: file, Status: "failed", Message: execErr.Error()})
			continue
		}

		// Mark as executed
		state.Executed[key] = SeedRecord{Hash: hash, RanAt: time.Now()}
		results = append(results, SeedResult{File: file, Status: "executed"})
	}

	if err := saveSeedState(state); err != nil {
		slog.Warn("failed to save seed state", "component", "seed", "err", err)
	}

	return results
}

func seedSQLServer(containerName, username, passwordEnv, file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	cmd := exec.Command("docker", sqlServerSeedDockerArgs(containerName, username, passwordEnv)...)
	cmd.Stdin = strings.NewReader(string(data))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", filepath.Base(file), strings.TrimSpace(string(out)))
	}
	return nil
}

func sqlServerSeedDockerArgs(containerName, username, passwordEnv string) []string {
	const runSQLCmd = `password="$(printenv "$1")"
if [ -z "$password" ]; then
  echo "$1 is empty in the SQL Server container" >&2
  exit 2
fi
export SQLCMDPASSWORD="$password"
exec /opt/mssql-tools18/bin/sqlcmd -S localhost -U "$2" -C -I`
	return []string{
		"exec", "-i", containerName,
		"/bin/sh", "-c", runSQLCmd,
		"orbit-seed", passwordEnv, username,
	}
}

func seedMongoDB(containerName, database, file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	args := []string{"exec", "-i", containerName, "mongosh", "--quiet"}
	if database != "" {
		args = append(args, database)
	}

	cmd := exec.Command("docker", args...)
	cmd.Stdin = strings.NewReader(string(data))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", filepath.Base(file), strings.TrimSpace(string(out)))
	}
	return nil
}
