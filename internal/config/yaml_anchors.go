package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// inlineAliases replaces every alias in the tree with a copy of its anchor and
// folds << merge keys into their mapping, so the result parses on its own.
// yaml.v3 only expands aliases while decoding into Go values, which would lose
// the line numbers that schema errors cite and the key order the merged file
// keeps; so the expansion is done on the node tree instead. Keys written next
// to << win, and in a list of merges the earlier entry wins, as in yaml.v3.
func inlineAliases(node *yaml.Node, expanding map[*yaml.Node]bool) (*yaml.Node, error) {
	if node.Kind == yaml.AliasNode {
		if expanding[node.Alias] {
			return nil, fmt.Errorf("line %d: anchor %q contains itself", node.Line, node.Value)
		}
		expanding[node.Alias] = true
		defer delete(expanding, node.Alias)
		return inlineAliases(node.Alias, expanding)
	}

	inlined := *node
	inlined.Anchor = ""
	inlined.Content = nil
	var mergeSources []*yaml.Node
	for i, child := range node.Content {
		if node.Kind == yaml.MappingNode && i%2 == 0 && isMergeKey(child) {
			mergeSources = append(mergeSources, node.Content[i+1])
			continue
		}
		if node.Kind == yaml.MappingNode && i%2 == 1 && isMergeKey(node.Content[i-1]) {
			continue
		}
		resolved, err := inlineAliases(child, expanding)
		if err != nil {
			return nil, err
		}
		inlined.Content = append(inlined.Content, resolved)
	}
	for _, source := range mergeSources {
		resolved, err := inlineAliases(source, expanding)
		if err != nil {
			return nil, err
		}
		mappings := []*yaml.Node{resolved}
		if resolved.Kind == yaml.SequenceNode {
			mappings = resolved.Content
		}
		for _, mapping := range mappings {
			if mapping.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("line %d: merge key << must reference a mapping or a list of mappings", source.Line)
			}
			for i := 0; i < len(mapping.Content); i += 2 {
				if mappingKeyIndex(&inlined, mapping.Content[i].Value) == -1 {
					inlined.Content = append(inlined.Content, mapping.Content[i], mapping.Content[i+1])
				}
			}
		}
	}
	return &inlined, nil
}

func isMergeKey(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.Value == "<<" && node.ShortTag() == "!!merge"
}
