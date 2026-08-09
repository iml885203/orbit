package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Extension sections are top-level env-YAML keys owned by feature sets
// (e.g. "claim") rather than by the core schema. The core keeps the file
// format stable — the key stays where it always was — but no longer
// knows what's inside: a registered decoder produces an opaque value
// that readers fetch back via Config.Extension.
//
// Registration must happen before ANY Load: config.Load runs on CLI
// startup paths and per daemon request, so sections register from
// package init in the owning feature package (the one deliberate
// exception to the no-init()-registry rule in the extension package doc
// — a section registered after the first Load would make identical
// files parse differently over time).
type ExtensionSection struct {
	// Decode turns the section's YAML node (env-substituted, from the
	// env file) into the section's value. Returning an error fails the
	// whole Load — decoders own their section's validation.
	Decode func(node *yaml.Node, cfgPath string) (any, error)
	// ValidateFragment checks schema shape only. Inherited fragments may omit
	// required fields that the merged section's Decode will validate.
	ValidateFragment func(node *yaml.Node) error
	// Default, when non-nil, runs if the env file has no such section —
	// the hook for shared sibling files (e.g. envs/data/claim.yaml).
	// A nil result means the feature is simply absent for this env.
	Default func(cfgPath string) (any, error)
	// Validate checks decoded feature intent against the complete environment.
	// It runs from Config.Validate after core containers and services exist.
	Validate func(value any, cfg *Config) error
}

func validateExtensionSections(cfg *Config) error {
	extensionSections.mu.RLock()
	defer extensionSections.mu.RUnlock()

	names := make([]string, 0, len(cfg.ext))
	for name := range cfg.ext {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		spec, ok := extensionSections.specs[name]
		if !ok || spec.Validate == nil {
			continue
		}
		if err := spec.Validate(cfg.ext[name], cfg); err != nil {
			return fmt.Errorf("%s section: %w", name, err)
		}
	}
	return nil
}

var extensionSections = struct {
	mu    sync.RWMutex
	specs map[string]ExtensionSection
}{specs: map[string]ExtensionSection{}}

func RegisterExtensionSection(name string, spec ExtensionSection) {
	extensionSections.mu.Lock()
	defer extensionSections.mu.Unlock()
	extensionSections.specs[name] = spec
}

// decodeExtensionSections enforces the section allowlist and runs the
// registered decoders. The allowlist check replaces strict decoding for
// top-level keys: the inline Extensions map swallows unknown keys before
// yaml.v3's KnownFields can reject them (decode.go routes them into the
// map), so without this check a typo'd top-level key would silently
// become an "extension section" instead of failing loudly.
func decodeExtensionSections(cfg *Config, cfgPath string) error {
	extensionSections.mu.RLock()
	defer extensionSections.mu.RUnlock()
	if err := validateExtensionSectionNamesLocked(cfg); err != nil {
		return err
	}

	for name, spec := range extensionSections.specs {
		node, present := cfg.Extensions[name]
		// A bare `claim:` (null value) means "no inline section": the old
		// schema decoded it to a nil pointer and fell through to the
		// shared default — key presence alone must not shadow Default.
		if present && (node.Kind == 0 || node.Tag == "!!null") {
			present = false
		}
		var (
			value any
			err   error
		)
		switch {
		case present:
			value, err = spec.Decode(&node, cfgPath)
		case spec.Default != nil:
			value, err = spec.Default(cfgPath)
		default:
			continue
		}
		if err != nil {
			return fmt.Errorf("%s section: %w", name, err)
		}
		if value == nil {
			continue
		}
		if cfg.ext == nil {
			cfg.ext = make(map[string]any)
		}
		cfg.ext[name] = value
	}
	return nil
}

func validateExtensionSectionNames(cfg *Config) error {
	extensionSections.mu.RLock()
	defer extensionSections.mu.RUnlock()
	return validateExtensionSectionNamesLocked(cfg)
}

func validateExtensionSectionNamesLocked(cfg *Config) error {

	names := make([]string, 0, len(cfg.Extensions))
	for name := range cfg.Extensions {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic first error under multiple typos
	for _, name := range names {
		if _, ok := extensionSections.specs[name]; !ok {
			if name == "previewOnly" {
				return fmt.Errorf("line %d: previewOnly was removed; delete this field because every environment can now be activated and managed", cfg.Extensions[name].Line)
			}
			known := coreTopLevelFields()
			for k := range extensionSections.specs {
				known = append(known, k)
			}
			sort.Strings(known)
			node := cfg.Extensions[name]
			hint := ""
			if suggestion := closestName(name, known); suggestion != "" {
				hint = fmt.Sprintf(` (did you mean %q?)`, suggestion)
			}
			return fmt.Errorf("line %d: unknown top-level section %s%s (available: %s)", node.Line, name, hint, strings.Join(known, ", "))
		}
	}
	return nil
}

func validateExtensionFragments(cfg *Config) error {
	extensionSections.mu.RLock()
	defer extensionSections.mu.RUnlock()

	names := make([]string, 0, len(cfg.Extensions))
	for name := range cfg.Extensions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		node := cfg.Extensions[name]
		spec, ok := extensionSections.specs[name]
		if !ok || spec.ValidateFragment == nil || node.Kind == 0 || node.Tag == "!!null" {
			continue
		}
		if err := spec.ValidateFragment(&node); err != nil {
			return fmt.Errorf("%s section: %w", name, err)
		}
	}
	return nil
}

// Extension returns the decoded value of a registered section, nil when
// the active env doesn't carry it. Callers type-assert to the type their
// own decoder produced.
func (c *Config) Extension(name string) any {
	return c.ext[name]
}

// WithExtension returns a copy of c with the named section value set —
// the same shallow-copy splice shape as WithContainer, for assembling
// configs without going through Load (tests, wiring). Published configs
// stay immutable: the copy gets a fresh ext map.
func (c *Config) WithExtension(name string, value any) *Config {
	next := *c
	next.ext = make(map[string]any, len(c.ext)+1)
	for k, v := range c.ext {
		next.ext[k] = v
	}
	next.ext[name] = value
	return &next
}

// ExpandEnv applies the same ${VAR:-default} substitution Load applies
// to env files. Exported for extension-section Default hooks that load
// shared sibling files and must substitute identically.
func ExpandEnv(input string) string {
	return substituteEnvVars(input)
}

// LoadSharedSiblingYAML decodes the shared envs/data/<filename> file next
// to cfgPath into out — the convention extension-section Default hooks use
// for team-shared config (for example claim.yaml). found is
// false when the file is absent or unreadable (the feature is simply not
// configured for this env, never a Load failure); the returned error is
// only a decode failure of a file that IS present, which callers may
// still choose to swallow. ${VAR} substitution and KnownFields strictness
// match Load, so a shared file parses identically to an inline section.
func LoadSharedSiblingYAML(cfgPath, filename string, out any) (found bool, err error) {
	abs, err := filepath.Abs(cfgPath)
	if err != nil {
		return false, nil
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(abs), "data", filename))
	if err != nil {
		return false, nil
	}
	dec := yaml.NewDecoder(strings.NewReader(ExpandEnv(string(data))))
	dec.KnownFields(true)
	return true, addSchemaFieldGuidance(dec.Decode(out), out)
}

// DecodeStrict decodes a section node into out with strict field
// checking. Section decoders must use this instead of node.Decode:
// yaml.v3's Node.Decode ignores KnownFields, so a typo'd key inside the
// section would otherwise be silently dropped — the same class of
// silent-typo regression the top-level allowlist exists to prevent.
func DecodeStrict(node *yaml.Node, out any) error {
	raw, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	err = addSchemaFieldGuidance(dec.Decode(out), out)
	return remapYAMLLineNumbers(err, authoredYAMLLineMap(raw, node))
}

var yamlLineNumberPattern = regexp.MustCompile(`\bline ([0-9]+)\b`)

func authoredYAMLLineMap(raw []byte, authored *yaml.Node) map[int]int {
	var normalized yaml.Node
	if err := yaml.Unmarshal(raw, &normalized); err != nil || len(normalized.Content) != 1 {
		return nil
	}
	lines := make(map[int]int)
	var collect func(*yaml.Node, *yaml.Node)
	collect = func(current, original *yaml.Node) {
		if current.Line > 0 && original.Line > 0 {
			lines[current.Line] = original.Line
		}
		limit := len(current.Content)
		if len(original.Content) < limit {
			limit = len(original.Content)
		}
		for i := 0; i < limit; i++ {
			collect(current.Content[i], original.Content[i])
		}
	}
	collect(normalized.Content[0], authored)
	return lines
}

func remapYAMLLineNumbers(err error, authoredLines map[int]int) error {
	if err == nil || len(authoredLines) == 0 {
		return err
	}
	message := yamlLineNumberPattern.ReplaceAllStringFunc(err.Error(), func(match string) string {
		parts := yamlLineNumberPattern.FindStringSubmatch(match)
		generatedLine, conversionErr := strconv.Atoi(parts[1])
		if conversionErr != nil {
			return match
		}
		authoredLine, ok := authoredLines[generatedLine]
		if !ok {
			return match
		}
		return "line " + strconv.Itoa(authoredLine)
	})
	return schemaGuidanceError{err: err, message: message}
}

func coreTopLevelFields() []string {
	fields := schemaFieldsByType(reflect.TypeOf(Config{}))[reflect.TypeOf(Config{}).String()]
	return append([]string(nil), fields...)
}
