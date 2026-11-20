package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
	"gopkg.in/yaml.v3"
)

func statusCommandEnvelope(t *testing.T, cfg *config.Config, running map[string]daemon.ResourceStatus, d daemonStatus) renderedStatusEnvelope {
	t.Helper()
	prepareLifecycleProgressTest(t)
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	project := t.TempDir()
	path := filepath.Join(project, "orbit.yaml")
	var document config.Config
	if cfg != nil {
		document = *cfg
	}
	document.Version = "3"
	raw, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := yaml.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"services", "containers"} {
		workloads, _ := input[section].(map[string]any)
		for _, value := range workloads {
			workload := value.(map[string]any)
			ports, _ := workload["ports"].(map[string]any)
			for label, value := range ports {
				port := value.(map[string]any)
				host := port["host"].(int)
				target := port["target"].(int)
				if target == 0 {
					ports[label] = host
				} else {
					ports[label] = strconv.Itoa(host) + ":" + strconv.Itoa(target)
				}
			}
		}
	}
	raw, err = yaml.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	previousVersion, previousBuildTime := version, buildTime
	t.Cleanup(func() { version, buildTime = previousVersion, previousBuildTime })
	if d.OnDisk != "" {
		version, buildTime, _ = strings.Cut(d.OnDisk, " (")
		buildTime = strings.TrimSuffix(buildTime, ")")
	}
	if d.Running {
		resources := make([]daemon.ResourceStatus, 0, len(running))
		for name := range running {
			resources = append(resources, running[name])
		}
		sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
		serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/health":
				w.WriteHeader(http.StatusOK)
			case "/api/status":
				selected := path
				if d.ContextMismatch {
					selected = d.ConfigPath
				}
				daemon.WriteJSON(w, http.StatusOK, daemon.StatusResponse{ConfigPath: selected, Resources: resources, ConfigStale: d.ConfigStale, ConfigStaleReason: d.ConfigStaleReason})
			case "/api/version":
				daemon.WriteJSON(w, http.StatusOK, daemon.VersionResponse{Running: d.Version})
			default:
				http.NotFound(w, r)
			}
		})
	}
	out, diagnostics, err := executeAppCommand(t, "status", "--json")
	if err != nil {
		t.Fatalf("status: %v\n%s\n%s", err, out, diagnostics)
	}
	assertOneLifecycleEnvelope(t, out, true)
	var envelope renderedStatusEnvelope
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("decode status: %v\n%s", err, out)
	}
	return envelope
}
