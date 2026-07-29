package config

import (
	"regexp"

	"gopkg.in/yaml.v3"
)

var autoPortExpression = regexp.MustCompile(`^\$\{ORBIT_AUTO_PORT_[A-Za-z0-9_]+:-[0-9]+\}(?::[0-9]+)?$`)

func markAutoPorts(source []byte, cfg *Config) {
	var document yaml.Node
	if err := yaml.Unmarshal(source, &document); err != nil || len(document.Content) == 0 {
		return
	}
	root := document.Content[0]
	markResourceAutoPorts(mappingValue(root, "containers"), func(name string) map[string]PortDef {
		if container := cfg.Containers[name]; container != nil {
			return container.Ports
		}
		return nil
	})
	markResourceAutoPorts(mappingValue(root, "services"), func(name string) map[string]PortDef {
		if service := cfg.Services[name]; service != nil {
			return service.Ports
		}
		return nil
	})
	markSidecarAutoPorts(mappingValue(root, "containers"), cfg)
}

func markResourceAutoPorts(section *yaml.Node, portsFor func(string) map[string]PortDef) {
	if section == nil || section.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(section.Content); i += 2 {
		name := section.Content[i].Value
		resource := section.Content[i+1]
		markPortMap(mappingValue(resource, "ports"), portsFor(name))
	}
}

func markSidecarAutoPorts(containers *yaml.Node, cfg *Config) {
	if containers == nil || containers.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(containers.Content); i += 2 {
		container := cfg.Containers[containers.Content[i].Value]
		if container == nil {
			continue
		}
		sidecars := mappingValue(containers.Content[i+1], "sidecars")
		if sidecars == nil || sidecars.Kind != yaml.SequenceNode {
			continue
		}
		for _, source := range sidecars.Content {
			nameNode := mappingValue(source, "name")
			if nameNode == nil {
				continue
			}
			for sidecarIndex := range container.Sidecars {
				if container.Sidecars[sidecarIndex].Name != nameNode.Value {
					continue
				}
				markPortMap(mappingValue(source, "ports"), container.Sidecars[sidecarIndex].Ports)
				break
			}
		}
	}
}

func markPortMap(source *yaml.Node, ports map[string]PortDef) {
	if source == nil || source.Kind != yaml.MappingNode || ports == nil {
		return
	}
	for i := 0; i+1 < len(source.Content); i += 2 {
		label := source.Content[i].Value
		value := source.Content[i+1]
		if value.Kind != yaml.ScalarNode || !autoPortExpression.MatchString(value.Value) {
			continue
		}
		port, ok := ports[label]
		if !ok {
			continue
		}
		port.auto = true
		port.preferred = port.Host
		ports[label] = port
	}
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
