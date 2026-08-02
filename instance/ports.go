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

	"github.com/iml885203/orbit/atomicio"
	"github.com/iml885203/orbit/config"
	orbitport "github.com/iml885203/orbit/port"
)

type portAssignments struct {
	UpdatedAt time.Time      `json:"updated_at"`
	Ports     map[string]int `json:"ports"`
}

func ResolvePorts(cfg *config.Config) error {
	name := CurrentName()
	if name == "" {
		return nil
	}
	return updatePortAssignments(name, func(assignments *portAssignments, reserved map[int]bool) error {
		for _, request := range configPortRequests(cfg) {
			if assigned := assignments.Ports[request.key]; assigned > 0 {
				request.apply(assigned)
				reserved[assigned] = true
				continue
			}
			assigned, err := choosePort(request.preferred, reserved)
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

func ResolveDashboardPort(preferred int) (int, error) {
	name := CurrentName()
	if name == "" {
		return preferred, nil
	}
	assigned := preferred
	err := updatePortAssignments(name, func(assignments *portAssignments, reserved map[int]bool) error {
		if current := assignments.Ports["dashboard"]; current > 0 {
			assigned = current
			return nil
		}
		var err error
		assigned, err = choosePort(preferred, reserved)
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
	preferred int
	apply     func(int)
}

func configPortRequests(cfg *config.Config) []portRequest {
	requests := make([]portRequest, 0)
	add := func(name string, ports map[string]config.PortDef, health *config.HealthCheckConfig) {
		labels := make([]string, 0, len(ports))
		for label := range ports {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		for _, label := range labels {
			label := label
			original := ports[label]
			requests = append(requests, portRequest{
				key: name + "/" + label, preferred: original.Host,
				apply: func(assigned int) {
					updated := ports[label]
					ports[label] = config.PortDef{Host: assigned, Target: updated.Target}
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
		add(name, container.Ports, container.HealthCheck)
		for i := range container.Sidecars {
			sidecar := &container.Sidecars[i]
			add(name+"/sidecar/"+sidecar.Name, sidecar.Ports, nil)
		}
	}
	serviceNames := make([]string, 0, len(cfg.Services))
	for name := range cfg.Services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)
	for _, name := range serviceNames {
		service := cfg.Services[name]
		add(name, service.Ports, service.HealthCheck)
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

func choosePort(preferred int, reserved map[int]bool) (int, error) {
	for candidate := preferred; candidate <= 65535; candidate++ {
		if reserved[candidate] || !portAvailable(candidate) {
			continue
		}
		return candidate, nil
	}
	for attempts := 0; attempts < 100; attempts++ {
		portNumber, err := orbitport.FindFree()
		if err != nil {
			return 0, err
		}
		if !reserved[portNumber] {
			return portNumber, nil
		}
	}
	return 0, errors.New("no unreserved port available")
}

func portAvailable(port int) bool {
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
