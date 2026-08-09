package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func loadInheritedDocument(path string) (*yaml.Node, error) {
	child, err := readExpandedDocument(path)
	if err != nil {
		return nil, err
	}
	parentReference, err := takeExtends(child, path)
	if err != nil || parentReference == "" {
		return child, err
	}

	parentPath := resolveExtendedPath(path, parentReference)
	parent, err := readExpandedDocument(parentPath)
	if err != nil {
		return nil, fmt.Errorf("loading parent env file %s: %w", parentPath, err)
	}
	parentReference, err = takeExtends(parent, parentPath)
	if err != nil {
		return nil, err
	}
	if parentReference != "" {
		return nil, fmt.Errorf("parsing env file %s: extends may only be used one level deep", parentPath)
	}
	if nullPath := inheritedNullPath(parent.Content[0], child.Content[0], nil); nullPath != "" {
		return nil, fmt.Errorf("parsing env file %s: inherited key %s cannot be null; extends does not support delete markers", path, nullPath)
	}

	merged := *child
	merged.Content = []*yaml.Node{mergeYAMLNodes(parent.Content[0], child.Content[0])}
	return &merged, nil
}

func inheritedNullPath(parent, child *yaml.Node, path []string) string {
	if parent.Kind != yaml.MappingNode || child.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i < len(child.Content); i += 2 {
		childKey, childValue := child.Content[i], child.Content[i+1]
		parentIndex := mappingKeyIndex(parent, childKey.Value)
		if parentIndex == -1 {
			continue
		}
		currentPath := make([]string, len(path)+1)
		copy(currentPath, path)
		currentPath[len(path)] = childKey.Value
		if childValue.Tag == "!!null" {
			return strings.Join(currentPath, ".")
		}
		if nested := inheritedNullPath(parent.Content[parentIndex+1], childValue, currentPath); nested != "" {
			return nested
		}
	}
	return ""
}

func readExpandedDocument(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(substituteEnvVars(string(data))), &document); err != nil {
		return nil, fmt.Errorf("parsing env file %s: %w", path, err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("parsing env file %s: top-level value must be a mapping", path)
	}
	return &document, nil
}

func takeExtends(document *yaml.Node, path string) (string, error) {
	mapping := document.Content[0]
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != "extends" {
			continue
		}
		value := mapping.Content[i+1]
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || strings.TrimSpace(value.Value) == "" {
			return "", fmt.Errorf("parsing env file %s: extends must be a non-empty file path", path)
		}
		mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
		return value.Value, nil
	}
	return "", nil
}

func resolveExtendedPath(childPath, parentReference string) string {
	if filepath.IsAbs(parentReference) {
		return filepath.Clean(parentReference)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(childPath), parentReference))
}

func mergeYAMLNodes(parent, child *yaml.Node) *yaml.Node {
	if parent.Kind != yaml.MappingNode || child.Kind != yaml.MappingNode {
		return child
	}

	merged := *parent
	merged.Content = append([]*yaml.Node(nil), parent.Content...)
	for i := 0; i < len(child.Content); i += 2 {
		childKey, childValue := child.Content[i], child.Content[i+1]
		parentIndex := mappingKeyIndex(&merged, childKey.Value)
		if parentIndex == -1 {
			merged.Content = append(merged.Content, childKey, childValue)
			continue
		}
		merged.Content[parentIndex] = childKey
		merged.Content[parentIndex+1] = mergeYAMLNodes(merged.Content[parentIndex+1], childValue)
	}
	return &merged
}

func mappingKeyIndex(mapping *yaml.Node, key string) int {
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func InheritanceFiles(path string) ([]string, error) {
	document, err := readExpandedDocument(path)
	if err != nil {
		return nil, err
	}
	parentReference, err := takeExtends(document, path)
	if err != nil || parentReference == "" {
		return []string{path}, err
	}
	return []string{path, resolveExtendedPath(path, parentReference)}, nil
}
