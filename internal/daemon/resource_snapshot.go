package daemon

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/iml885203/orbit/config"
)

func (s *Server) handleResources(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}

	// One immutable snapshot for the whole aggregation — published
	// configs are never mutated, so no lock is needed.
	cfg := s.holder.Load()
	resources := snapshotWorkloads(dependencyMap(cfg), s.computeStatuses(cfg))
	resources = append(resources, snapshotExternals(cfg)...)
	// Feature-owned resources (tunnels + routes today) arrive through
	// contributors registered in DaemonSetup.
	for _, contribute := range s.resourceContributors {
		resources = append(resources, contribute(r.Context())...)
	}

	sortResources(resources)
	writeJSON(w, http.StatusOK, ResourcesResponse{
		SchemaVersion: ResourceSchemaVersion,
		Env:           s.currentEnvName(),
		Resources:     resources,
	})
}

// dependencyMap copies every workload's depends_on list out of one
// immutable snapshot.
func dependencyMap(cfg *config.Config) map[string][]string {
	deps := make(map[string][]string, len(cfg.Services)+len(cfg.Containers))
	for name := range cfg.Services {
		if d := cfg.GetDependencies(name); len(d) > 0 {
			deps[name] = append([]string(nil), d...)
		}
	}
	for name := range cfg.Containers {
		if d := cfg.GetDependencies(name); len(d) > 0 {
			deps[name] = append([]string(nil), d...)
		}
	}
	return deps
}

// snapshotWorkloads maps the status view of containers and services into
// snapshots, enriching them with the copied dependency edges.
func snapshotWorkloads(deps map[string][]string, statuses []ResourceStatus) []ResourceSnapshot {
	out := make([]ResourceSnapshot, 0, len(statuses))
	for i := range statuses {
		st := &statuses[i]
		props := map[string]string{}
		if st.Image != "" {
			props["image"] = st.Image
		}
		if st.Mode != "" {
			props["mode"] = st.Mode
		}
		if st.Uptime != "" {
			props["uptime"] = st.Uptime
		}
		if st.StartupTime != "" {
			props["startup_time"] = st.StartupTime
		}
		if st.RestartCount > 0 {
			props["restarts"] = strconv.Itoa(st.RestartCount)
		}
		for label, port := range st.Ports {
			props["port:"+label] = strconv.Itoa(port)
		}
		for _, sc := range st.Sidecars {
			props["sidecar:"+sc.Name] = sc.URL
		}
		var urls []string
		if st.URL != "" {
			urls = append(urls, st.URL)
		}
		out = append(out, ResourceSnapshot{
			Name:        st.Name,
			Type:        string(st.Kind),
			State:       st.State,
			StateReason: resourceFailureSummary(*st),
			DependsOn:   deps[st.Name],
			URLs:        urls,
			Properties:  emptyAsNil(props),
			Health:      st.HealthProgress,
		})
	}
	return out
}

// snapshotExternals renders the async-graph placeholders. They have no
// lifecycle, so State stays empty and the kafka declarations become
// properties.
func snapshotExternals(cfg *config.Config) []ResourceSnapshot {
	out := make([]ResourceSnapshot, 0, len(cfg.Externals))
	for name, ext := range cfg.Externals {
		if ext == nil {
			continue
		}
		props := map[string]string{}
		if ext.Label != "" {
			props["label"] = ext.Label
		}
		if len(ext.Kafka.Produces) > 0 {
			props["kafka_produces"] = strings.Join(ext.Kafka.Produces, ", ")
		}
		if len(ext.Kafka.Consumes) > 0 {
			props["kafka_consumes"] = strings.Join(ext.Kafka.Consumes, ", ")
		}
		out = append(out, ResourceSnapshot{
			Name:       name,
			Type:       "external",
			Properties: emptyAsNil(props),
		})
	}
	return out
}

// sortResources orders deterministically: parents before children, then by
// type and name — consumers shouldn't reshuffle between polls.
func sortResources(resources []ResourceSnapshot) {
	sort.Slice(resources, func(i, j int) bool {
		a, b := resources[i], resources[j]
		if (a.Parent == "") != (b.Parent == "") {
			return a.Parent == ""
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.Name < b.Name
	})
}

func emptyAsNil(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}
