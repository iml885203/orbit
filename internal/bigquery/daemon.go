package bigquery

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/extension"
	"net/http"
	"sort"
	"time"
)

type ProfileSummary struct {
	Name        string `json:"name"`
	Mode        string `json:"mode"`
	JobProject  string `json:"job_project"`
	DataProject string `json:"data_project"`
	Location    string `json:"location"`
	Dataset     string `json:"dataset,omitempty"`
}

type BQSnapshot struct {
	Configured bool             `json:"configured"`
	Profiles   []ProfileSummary `json:"profiles"`
	Records    []Receipt        `json:"records"`
	Source     string           `json:"source"`
	ObservedAt string           `json:"observed_at"`
	Error      string           `json:"error,omitempty"`
}

func localSnapshot(cfg *config.Config) BQSnapshot {
	result := BQSnapshot{Profiles: []ProfileSummary{}, Records: []Receipt{}, Source: "local_canonical_records_no_cloud_check", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	c := From(cfg)
	if c == nil {
		return result
	}
	result.Configured = true
	for name, p := range c.Profiles {
		result.Profiles = append(result.Profiles, ProfileSummary{Name: name, Mode: p.Mode, JobProject: p.JobProject, DataProject: p.DataProject, Location: p.Location, Dataset: p.Dataset})
	}
	sort.Slice(result.Profiles, func(i, j int) bool { return result.Profiles[i].Name < result.Profiles[j].Name })
	records, err := readRecords(storeDirectory(c.Identity))
	if err != nil {
		result.Error = "Local recovery records could not be read; use the CLI to diagnose."
	} else {
		for _, rec := range records {
			result.Records = append(result.Records, *rec)
		}
	}
	return result
}

func SetupDaemon(host extension.Host, mux *http.ServeMux) extension.DaemonHooks {
	mux.HandleFunc("GET /api/features/bigquery", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(localSnapshot(host.Config()))
	})
	if registrar, ok := host.(daemon.DoctorRegistrar); ok {
		registrar.AddDoctorChecks(func() []daemon.DoctorCheck { return DoctorChecks(host.Config(), "") })
	}
	if registrar, ok := host.(daemon.ResourceRegistrar); ok {
		registrar.AddResourceContributor(func(_ context.Context) []daemon.ResourceSnapshot {
			snapshot := localSnapshot(host.Config())
			result := make([]daemon.ResourceSnapshot, 0, len(snapshot.Profiles))
			for _, p := range snapshot.Profiles {
				result = append(result, daemon.ResourceSnapshot{Name: "bq:" + p.Name, Type: "external", State: "configured", StateReason: "Cloud access and application readiness have not been checked", Properties: map[string]string{"feature": "bigquery", "mode": p.Mode, "job_project": p.JobProject, "data_project": p.DataProject, "location": p.Location, "source": snapshot.Source, "observed_at": snapshot.ObservedAt}})
			}
			return result
		})
	}
	return extension.DaemonHooks{}
}

func DoctorChecks(cfg *config.Config, _ string) []daemon.DoctorCheck {
	c := From(cfg)
	if c == nil {
		return nil
	}
	snapshot := localSnapshot(cfg)
	status := daemon.CheckInfo
	message := "Profiles configured; ADC, IAM, SQL and application readiness require explicit checks."
	if snapshot.Error != "" {
		status = daemon.CheckWarn
		message = snapshot.Error
	}
	return []daemon.DoctorCheck{{Name: "BigQuery", Status: status, Message: message, Hint: "Use orbit bq inspect PROFILE --json for shared metadata; orbit bq sandbox list --json reads local recovery evidence."}}
}

func PrintDoctorChecks(cfg *config.Config) {
	for _, check := range DoctorChecks(cfg, "") {
		fmt.Printf("  BigQuery: %s\n", check.Message)
	}
}
