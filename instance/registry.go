package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/iml885203/orbit/atomicio"
	"github.com/iml885203/orbit/platform"
)

const manifestFile = "instance.json"

type Manifest struct {
	Name       string    `json:"name"`
	Namespace  string    `json:"namespace"`
	ConfigPath string    `json:"config_path"`
	CreatedAt  time.Time `json:"created_at"`
}

type Summary struct {
	Name        string            `json:"name"`
	Environment string            `json:"environment,omitempty"`
	State       string            `json:"state"`
	PID         int               `json:"pid,omitempty"`
	Dashboard   string            `json:"dashboard,omitempty"`
	SocketPath  string            `json:"socket_path"`
	ConfigPath  string            `json:"config_path,omitempty"`
	Namespace   string            `json:"namespace"`
	Endpoints   map[string]string `json:"endpoints,omitempty"`
}

func WriteManifest(configPath string) error {
	name := CurrentName()
	if name == "" {
		return nil
	}
	runtime, err := Resolve(BaseHome(), name)
	if err != nil {
		return err
	}
	manifest := Manifest{Name: name, Namespace: runtime.Namespace, ConfigPath: configPath, CreatedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding instance manifest: %w", err)
	}
	if err := atomicio.WriteFile(filepath.Join(runtime.Home, manifestFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing instance manifest: %w", err)
	}
	return nil
}

func ReadManifest(baseHome, name string) (Manifest, error) {
	runtime, err := Resolve(baseHome, name)
	if err != nil {
		return Manifest{}, err
	}
	data, err := os.ReadFile(filepath.Join(runtime.Home, manifestFile))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decoding instance manifest: %w", err)
	}
	return manifest, nil
}

func List(baseHome string) ([]Summary, error) {
	root := filepath.Join(baseHome, "instances")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []Summary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}
	instances := make([]Summary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || ValidateName(entry.Name()) != nil {
			continue
		}
		runtime, _ := Resolve(baseHome, entry.Name())
		manifest, manifestErr := ReadManifest(baseHome, entry.Name())
		// A directory with no manifest is a leftover, not an instance:
		// `clean` reads the manifest first and reports "does not exist",
		// so listing it offers a name no command can act on. Skipping
		// keeps both surfaces on one definition of existing, and drains
		// the residue an interrupted clean already left behind.
		if manifestErr != nil {
			continue
		}
		pid, dashboardPort := readOwnership(runtime.Home)
		state := "stopped"
		if platform.IsProcessAlive(pid) {
			state = "running"
		}
		dashboard := ""
		if dashboardPort > 0 && state == "running" {
			dashboard = fmt.Sprintf("http://localhost:%d", dashboardPort)
		}
		instances = append(instances, Summary{
			Name: entry.Name(), Environment: environmentName(manifest.ConfigPath), State: state,
			PID: pid, Dashboard: dashboard, SocketPath: filepath.Join(runtime.Home, "orbit.sock"),
			ConfigPath: manifest.ConfigPath, Namespace: runtime.Namespace,
		})
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].Name < instances[j].Name })
	return instances, nil
}

// RemoveResidue deletes an instance home that has no manifest — the state an
// interrupted clean leaves behind. Reports whether anything was there, so the
// caller can tell "finished a half-done clean" from "never existed".
func RemoveResidue(baseHome, name string) (bool, error) {
	runtime, err := Resolve(baseHome, name)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(runtime.Home); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := os.RemoveAll(runtime.Home); err != nil {
		return false, fmt.Errorf("removing instance residue: %w", err)
	}
	return true, nil
}

func RemoveHome(baseHome, name string) error {
	runtime, err := Resolve(baseHome, name)
	if err != nil {
		return err
	}
	return os.RemoveAll(runtime.Home)
}

func readOwnership(home string) (int, int) {
	data, err := os.ReadFile(filepath.Join(home, "orbit.pid"))
	if err != nil {
		return 0, 0
	}
	var record struct {
		PID           int `json:"pid"`
		DashboardPort int `json:"dashboard_port"`
	}
	if json.Unmarshal(data, &record) != nil {
		return 0, 0
	}
	return record.PID, record.DashboardPort
}

func environmentName(configPath string) string {
	base := filepath.Base(configPath)
	return base[:len(base)-len(filepath.Ext(base))]
}
