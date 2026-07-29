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

func seedStateFile() string {
	if home := os.Getenv("ORBIT_HOME"); home != "" {
		return filepath.Join(home, "seed-state.json")
	}
	if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
		return filepath.Join(localApp, "orbit", "seed-state.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".orbit", "seed-state.json")
}

func loadSeedState() *SeedState {
	data, err := os.ReadFile(seedStateFile())
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
	path := seedStateFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func seedFingerprint(command, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = h.Write([]byte(command))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
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

		hash, err := seedFingerprint(cfg.Seed.Command, file)
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

		containerName := ContainerName(os.Getenv("ORBIT_NAMESPACE"), name)
		if execErr := seedWithCommand(containerName, cfg.Seed.Command, file); execErr != nil {
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

func seedWithCommand(containerName, command, file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	cmd := exec.Command("docker", seedDockerArgs(containerName, command)...)
	cmd.Stdin = strings.NewReader(string(data))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", filepath.Base(file), strings.TrimSpace(string(out)))
	}
	return nil
}

func seedDockerArgs(containerName, command string) []string {
	return []string{
		"exec", "-i", containerName,
		"/bin/sh", "-c", command,
	}
}
