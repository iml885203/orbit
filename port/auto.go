package port

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"

	"github.com/iml885203/orbit/config"
)

type AutoResolution struct {
	Resource  string
	Label     string
	Preferred int
	Actual    int
}

type ExistingContainerPort func(name string, target int) (host int, found bool, err error)

type autoPortSpec struct {
	resource        string
	runtimeName     string
	label           string
	preferred       int
	target          int
	existing        ExistingContainerPort
	apply           func(int)
	updateReadiness func(int, int)
}

func ResolveAutoPorts(cfg *config.Config, existing ExistingContainerPort, runtimeReserved ...int) ([]AutoResolution, error) {
	if cfg == nil {
		return nil, nil
	}
	reserved := fixedPorts(cfg, runtimeReserved)
	specs := autoPortSpecs(cfg, existing)
	sort.Slice(specs, func(i, j int) bool {
		if specs[i].resource != specs[j].resource {
			return specs[i].resource < specs[j].resource
		}
		return specs[i].label < specs[j].label
	})

	var resolutions []AutoResolution
	for _, spec := range specs {
		actual, err := resolveAutoPort(spec, reserved)
		if err != nil {
			return nil, err
		}
		reserved[actual] = true
		spec.apply(actual)
		spec.updateReadiness(spec.preferred, actual)
		if actual != spec.preferred {
			resolutions = append(resolutions, AutoResolution{
				Resource:  spec.resource,
				Label:     spec.label,
				Preferred: spec.preferred,
				Actual:    actual,
			})
		}
	}
	return resolutions, nil
}

func resolveAutoPort(spec autoPortSpec, reserved map[int]bool) (int, error) {
	if spec.existing != nil {
		host, found, err := spec.existing(spec.runtimeName, spec.target)
		if err != nil {
			return 0, fmt.Errorf("inspecting managed port for %s: %w", spec.resource, err)
		}
		if found && !reserved[host] {
			return host, nil
		}
	}
	if !reserved[spec.preferred] && Available(spec.preferred) {
		return spec.preferred, nil
	}
	for candidate := spec.preferred + 1; candidate <= 65535; candidate++ {
		if !reserved[candidate] && Available(candidate) {
			return candidate, nil
		}
	}
	for candidate := 1024; candidate < spec.preferred; candidate++ {
		if !reserved[candidate] && Available(candidate) {
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("no available host port for %s.%s", spec.resource, spec.label)
}

func Available(portNumber int) bool {
	if portNumber < 1 || portNumber > 65535 {
		return false
	}
	_, _, inUse := isPortInUse(portNumber)
	return !inUse
}

func fixedPorts(cfg *config.Config, runtimeReserved []int) map[int]bool {
	reserved := make(map[int]bool)
	for _, portNumber := range runtimeReserved {
		if portNumber > 0 {
			reserved[portNumber] = true
		}
	}
	add := func(ports map[string]config.PortDef) {
		for _, definition := range ports {
			if !definition.IsAuto() {
				reserved[definition.Host] = true
			}
		}
	}
	for _, container := range cfg.Containers {
		add(container.Ports)
		for sidecarIndex := range container.Sidecars {
			add(container.Sidecars[sidecarIndex].Ports)
		}
	}
	for _, service := range cfg.Services {
		add(service.Ports)
	}
	return reserved
}

func autoPortSpecs(cfg *config.Config, existing ExistingContainerPort) []autoPortSpec {
	var specs []autoPortSpec
	for name, container := range cfg.Containers {
		for label, definition := range container.Ports {
			if !definition.IsAuto() {
				continue
			}
			name, label, definition := name, label, definition
			specs = append(specs, autoPortSpec{
				resource:    name,
				runtimeName: name,
				label:       label,
				preferred:   definition.PreferredHost(),
				target:      definition.Target,
				existing:    existing,
				apply: func(actual int) {
					next := container.Ports[label]
					next.Host = actual
					container.Ports[label] = next
				},
				updateReadiness: func(preferred, actual int) {
					updateHealthPort(container.HealthCheck, preferred, actual)
				},
			})
		}
		for sidecarIndex := range container.Sidecars {
			sidecar := &container.Sidecars[sidecarIndex]
			for label, definition := range sidecar.Ports {
				if !definition.IsAuto() {
					continue
				}
				label, definition := label, definition
				specs = append(specs, autoPortSpec{
					resource:    name + "/" + sidecar.Name,
					runtimeName: name + "-" + sidecar.Name,
					label:       label,
					preferred:   definition.PreferredHost(),
					target:      definition.Target,
					existing:    existing,
					apply: func(actual int) {
						next := sidecar.Ports[label]
						next.Host = actual
						sidecar.Ports[label] = next
					},
					updateReadiness: func(_, _ int) {},
				})
			}
		}
	}
	for name, service := range cfg.Services {
		for label, definition := range service.Ports {
			if !definition.IsAuto() {
				continue
			}
			name, label, definition := name, label, definition
			specs = append(specs, autoPortSpec{
				resource:  name,
				label:     label,
				preferred: definition.PreferredHost(),
				target:    definition.Target,
				apply: func(actual int) {
					next := service.Ports[label]
					next.Host = actual
					service.Ports[label] = next
				},
				updateReadiness: func(preferred, actual int) {
					updateHealthPort(service.HealthCheck, preferred, actual)
					service.URL = updateLoopbackURLPort(service.URL, preferred, actual)
				},
			})
		}
	}
	return specs
}

func updateHealthPort(check *config.HealthCheckConfig, preferred, actual int) {
	if check != nil && check.Port == preferred {
		check.Port = actual
	}
}

func updateLoopbackURLPort(raw string, preferred, actual int) string {
	if raw == "" || preferred == actual {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return raw
	}
	host := parsed.Hostname()
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return raw
	}
	portNumber, err := strconv.Atoi(parsed.Port())
	if err != nil || portNumber != preferred {
		return raw
	}
	parsed.Host = net.JoinHostPort(host, strconv.Itoa(actual))
	return parsed.String()
}
