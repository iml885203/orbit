package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// applyExtends resolves childYAML's `extends:` reference against the child
// file's directory and returns the merged document text. Both inputs are
// substituted independently before parsing — the parent must expand exactly
// as it does when loaded standalone, so a shared team file behaves the same
// whether or not something extends it. Marshaling the merged tree back to
// text discards source positions: a post-merge error that cites a line
// number refers to the synthetic merged document, not to either file. That
// is the accepted cost of reusing the single-file decode pipeline unchanged.
func applyExtends(childYAML, ref, childPath string) (string, error) {
	parentPath, err := resolveExtendsPath(ref, childPath)
	if err != nil {
		return "", err
	}
	parentData, err := os.ReadFile(parentPath)
	if err != nil {
		return "", fmt.Errorf("extends %q: %w", ref, err)
	}
	parentYAML := substituteEnvVars(string(parentData))

	var parentHeader struct {
		Extends string `yaml:"extends"`
	}
	if err := yaml.Unmarshal([]byte(parentYAML), &parentHeader); err != nil {
		return "", fmt.Errorf("extends %q: %w", ref, err)
	}
	if parentHeader.Extends != "" {
		return "", fmt.Errorf("extends %q: that file extends %q itself — extends is single-level, so extend a file that stands alone", ref, parentHeader.Extends)
	}

	var parentDoc, childDoc yaml.Node
	if err := yaml.Unmarshal([]byte(parentYAML), &parentDoc); err != nil {
		return "", fmt.Errorf("extends %q: %w", ref, err)
	}
	if err := yaml.Unmarshal([]byte(childYAML), &childDoc); err != nil {
		return "", err
	}
	merged := overlayNode(documentRoot(&parentDoc), documentRoot(&childDoc))
	out, err := yaml.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("extends %q: %w", ref, err)
	}
	return string(out), nil
}

func resolveExtendsPath(ref, childPath string) (string, error) {
	if filepath.IsAbs(ref) {
		return "", fmt.Errorf("extends %q: must be a path relative to the extending file's directory", ref)
	}
	absChild, err := filepath.Abs(childPath)
	if err != nil {
		return "", fmt.Errorf("extends %q: %w", ref, err)
	}
	parentPath := filepath.Join(filepath.Dir(absChild), ref)
	if filepath.Clean(parentPath) == filepath.Clean(absChild) {
		return "", fmt.Errorf("extends %q: an env file cannot extend itself", ref)
	}
	return parentPath, nil
}

// overlayNode merges child onto parent. Mappings merge key-by-key,
// recursing; every other kind — scalars, sequences, explicit nulls — is
// replaced by the child's node wholesale. Sequence replacement is a
// deliberate contract choice: element-wise rules (append? match by index?)
// are exactly the unpredictable deep-merge semantics extends excludes.
func overlayNode(parent, child *yaml.Node) *yaml.Node {
	if parent == nil {
		return child
	}
	if parent.Kind != yaml.MappingNode || child.Kind != yaml.MappingNode {
		return child
	}
	merged := *parent
	merged.Content = append([]*yaml.Node(nil), parent.Content...)
	for i := 0; i+1 < len(child.Content); i += 2 {
		key, val := child.Content[i], child.Content[i+1]
		idx := mappingValueIndex(merged.Content, key.Value)
		if idx == -1 {
			merged.Content = append(merged.Content, key, val)
			continue
		}
		merged.Content[idx] = overlayNode(merged.Content[idx], val)
	}
	return &merged
}

// mappingValueIndex returns the index of the VALUE node paired with key in a
// mapping's flattened [k1, v1, k2, v2, …] content, or -1.
func mappingValueIndex(content []*yaml.Node, key string) int {
	for i := 0; i+1 < len(content); i += 2 {
		if content[i].Value == key {
			return i + 1
		}
	}
	return -1
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		return doc.Content[0]
	}
	if doc.Kind == 0 {
		return nil
	}
	return doc
}
