package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/iml885203/orbit/internal/atomicio"
	"github.com/iml885203/orbit/internal/config"
	orbitport "github.com/iml885203/orbit/internal/port"
)

type portAssignments struct {
	UpdatedAt time.Time      `json:"updated_at"`
	Ports     map[string]int `json:"ports"`
}

// Survivors names what this instance left running past its last daemon, so a
// stored port it still holds is not mistaken for a port someone else took.
// Containers outlive `orbit daemon stop` by design, and host processes outlive
// a daemon crash until the new daemon retires them.
type Survivors struct {
	// Processes holds, by config.Services name, the host services whose
	// recorded process is still alive. Every stored port of such a service
	// counts as held, since the process may bind any of them.
	Processes map[string]bool
	// ContainerPorts reports the host ports this instance's running
	// containers publish. It is called at most once, and only when a stored
	// container port is taken, so Docker stays off the common path.
	ContainerPorts func() (map[int]bool, error)
}

// ResolvePorts applies this instance's stored ports to cfg, keeping each one
// while it is free or held by the instance's own survivors, and moving it to
// a free port otherwise. defaultDashboardPort is the default runtime's
// dashboard, which no instance resource may take.
func ResolvePorts(cfg *config.Config, defaultDashboardPort int, survivors Survivors) error {
	name := CurrentName()
	if name == "" {
		return nil
	}
	heldBySurvivor := survivors.holds()
	return updatePortAssignments(name, func(assignments *portAssignments, reserved map[int]bool) error {
		requests := configPortRequests(cfg)
		current := map[string]bool{"dashboard": true}
		for _, request := range requests {
			current[request.key] = true
		}
		// A key the config no longer has would otherwise keep its port from
		// this instance's dashboard and from every other instance.
		for key := range assignments.Ports {
			if !current[key] {
				delete(assignments.Ports, key)
			}
		}
		reserved[defaultDashboardPort] = true
		// The dashboard is assigned before the daemon resolves service ports
		// and is not a config request, so a relocated service could take the
		// very port the daemon is about to bind.
		if dashboard := assignments.Ports["dashboard"]; dashboard > 0 {
			reserved[dashboard] = true
		}
		owner := map[int]string{}
		for _, request := range requests {
			assigned := assignments.Ports[request.key]
			if assigned <= 0 || reserved[assigned] || owner[assigned] != "" {
				continue
			}
			if !orbitport.Available(assigned) && !heldBySurvivor(request, assigned) {
				continue
			}
			owner[assigned] = request.key
		}
		for port := range owner {
			reserved[port] = true
		}
		for _, request := range requests {
			// Reuse keeps restarts stable, unless an earlier run handed this key
			// a port it now shares with the dashboard or another key, or
			// something outside this instance now holds it.
			if assigned := assignments.Ports[request.key]; assigned > 0 && owner[assigned] == request.key {
				request.apply(assigned)
				continue
			}
			assigned, err := choosePort(request.preferred, reserved, orbitport.Available)
			if err != nil {
				return fmt.Errorf("allocating port for %s: %w", request.key, err)
			}
			assignments.Ports[request.key] = assigned
			reserved[assigned] = true
			request.apply(assigned)
		}
		return nil
	})
}

// holds reports whether a taken port belongs to one of the survivors. A
// failed container query counts as held: moving a port our own container
// still publishes would replace that container, while keeping a foreign one
// only surfaces the start-time port conflict.
func (s Survivors) holds() func(portRequest, int) bool {
	var (
		queried        bool
		containerPorts map[int]bool
		queryErr       error
	)
	return func(request portRequest, port int) bool {
		if !request.container {
			return s.Processes[request.resource]
		}
		if s.ContainerPorts == nil {
			return false
		}
		if !queried {
			queried = true
			containerPorts, queryErr = s.ContainerPorts()
		}
		return queryErr != nil || containerPorts[port]
	}
}

// ResolveDashboardPort returns the instance's dashboard port. The stored port
// is kept while it is free; a first or displaced assignment starts at
// preferred and avoids defaultDashboardPort, the default runtime's dashboard.
func ResolveDashboardPort(preferred int, defaultDashboardPort int) (int, error) {
	name := CurrentName()
	if name == "" {
		return preferred, nil
	}
	assigned := preferred
	err := updatePortAssignments(name, func(assignments *portAssignments, reserved map[int]bool) error {
		reserved[defaultDashboardPort] = true
		// The only caller starts a daemon after EnsureDaemon found none alive,
		// so a taken port is never held by this instance's own dashboard.
		if current := assignments.Ports["dashboard"]; current > 0 && !reserved[current] && loopbackAvailable(current) {
			assigned = current
			return nil
		}
		// Service ports already assigned to this instance are free while it is
		// stopped, but they are spoken for.
		for key, port := range assignments.Ports {
			if key != "dashboard" {
				reserved[port] = true
			}
		}
		var err error
		assigned, err = choosePort(preferred, reserved, loopbackAvailable)
		if err != nil {
			return fmt.Errorf("allocating dashboard port: %w", err)
		}
		assignments.Ports["dashboard"] = assigned
		return nil
	})
	return assigned, err
}

func updatePortAssignments(name string, update func(*portAssignments, map[int]bool) error) error {
	baseHome := BaseHome()
	unlock, err := acquirePortLock(baseHome)
	if err != nil {
		return err
	}
	defer unlock()
	runtime, err := Resolve(baseHome, name)
	if err != nil {
		return err
	}
	path := filepath.Join(runtime.Home, "ports.json")
	assignments := readPortAssignments(path)
	if err := update(&assignments, reservedInstancePorts(baseHome, name)); err != nil {
		return err
	}
	assignments.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(assignments, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding instance ports: %w", err)
	}
	if err := atomicio.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing instance ports: %w", err)
	}
	return nil
}

type portRequest struct {
	key       string
	resource  string
	container bool
	preferred int
	apply     func(int)
}

func configPortRequests(cfg *config.Config) []portRequest {
	requests := make([]portRequest, 0)
	add := func(name string, isContainer bool, ports map[string]config.PortDef, health *config.HealthCheckConfig, applicationURL *string) {
		labels := make([]string, 0, len(ports))
		for label := range ports {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		for _, label := range labels {
			label := label
			original := ports[label]
			requests = append(requests, portRequest{
				key: name + "/" + label, resource: name, container: isContainer, preferred: original.Host,
				apply: func(assigned int) {
					updated := ports[label]
					ports[label] = config.PortDef{Host: assigned, Target: updated.Target}
					if applicationURL != nil {
						*applicationURL = config.RemapApplicationURL(*applicationURL, original.Host, assigned)
					}
					if health != nil && health.Port == original.Host {
						health.Port = assigned
					}
				},
			})
		}
	}
	containerNames := make([]string, 0, len(cfg.Containers))
	for name := range cfg.Containers {
		containerNames = append(containerNames, name)
	}
	sort.Strings(containerNames)
	for _, name := range containerNames {
		container := cfg.Containers[name]
		add(name, true, container.Ports, container.HealthCheck, &container.URL)
		for i := range container.Sidecars {
			sidecar := &container.Sidecars[i]
			add(name+"/sidecar/"+sidecar.Name, true, sidecar.Ports, nil, nil)
		}
	}
	serviceNames := make([]string, 0, len(cfg.Services))
	for name := range cfg.Services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)
	for _, name := range serviceNames {
		service := cfg.Services[name]
		add(name, false, service.Ports, service.HealthCheck, &service.URL)
	}
	return requests
}

func readPortAssignments(path string) portAssignments {
	assignments := portAssignments{Ports: make(map[string]int)}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &assignments)
	}
	if assignments.Ports == nil {
		assignments.Ports = make(map[string]int)
	}
	return assignments
}

func reservedInstancePorts(baseHome, currentName string) map[int]bool {
	reserved := make(map[int]bool)
	entries, _ := os.ReadDir(filepath.Join(baseHome, "instances"))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == currentName {
			continue
		}
		assignments := readPortAssignments(filepath.Join(baseHome, "instances", entry.Name(), "ports.json"))
		if time.Since(assignments.UpdatedAt) > 24*time.Hour {
			continue
		}
		for _, port := range assignments.Ports {
			reserved[port] = true
		}
	}
	return reserved
}

func choosePort(preferred int, reserved map[int]bool, available func(int) bool) (int, error) {
	for candidate := preferred; candidate <= 65535; candidate++ {
		if reserved[candidate] || !available(candidate) {
			continue
		}
		return candidate, nil
	}
	for attempts := 0; attempts < 100; attempts++ {
		portNumber, err := orbitport.FindFree()
		if err != nil {
			return 0, err
		}
		if !reserved[portNumber] && available(portNumber) {
			return portNumber, nil
		}
	}
	return 0, errors.New("no unreserved port available")
}

// loopbackAvailable is the dashboard's probe: the dashboard only binds
// loopback, so a listener elsewhere must not move it the way port.Available
// would move a resource port.
func loopbackAvailable(port int) bool {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func acquirePortLock(baseHome string) (func(), error) {
	instancesDir := filepath.Join(baseHome, "instances")
	if err := os.MkdirAll(instancesDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating instances directory: %w", err)
	}
	lockPath := filepath.Join(instancesDir, ".port-lock")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := os.Mkdir(lockPath, 0o700); err == nil {
			return func() { _ = os.Remove(lockPath) }, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("locking instance ports: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for instance port allocation lock")
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(lockPath)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
