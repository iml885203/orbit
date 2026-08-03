package envsource

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/atomicio"
)

const (
	TypeGit   = "git"
	TypeLocal = "local"
)

var (
	ErrNotFound = errors.New("environment source not found")
	namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

type Source struct {
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	URL           string    `json:"url,omitempty"`
	Path          string    `json:"path,omitempty"`
	Ref           string    `json:"ref,omitempty"`
	Workspace     string    `json:"workspace,omitempty"`
	Default       bool      `json:"default,omitempty"`
	ResolvedRef   string    `json:"resolved_ref,omitempty"`
	Commit        string    `json:"commit,omitempty"`
	LastSyncAt    time.Time `json:"last_sync_at,omitempty"`
	LastSyncError string    `json:"last_sync_error,omitempty"`
}

func (s Source) Location() string {
	if s.Type == TypeLocal {
		return s.Path
	}
	return s.URL
}

func (s Source) Validate() error {
	if !namePattern.MatchString(s.Name) || s.Name == "." || s.Name == ".." {
		return fmt.Errorf("source name %q must match [A-Za-z0-9][A-Za-z0-9._-]*", s.Name)
	}
	switch s.Type {
	case TypeGit:
		if strings.TrimSpace(s.URL) == "" {
			return errors.New("Git source requires a URL")
		}
		if s.Path != "" {
			return errors.New("Git source cannot have a local path")
		}
	case TypeLocal:
		if strings.TrimSpace(s.Path) == "" {
			return errors.New("local source requires a path")
		}
		if s.URL != "" || s.Ref != "" {
			return errors.New("local source cannot have a Git URL or ref")
		}
	default:
		return fmt.Errorf("unknown source type %q", s.Type)
	}
	return nil
}

type Registry struct {
	path    string
	Sources []Source `json:"sources"`
}

func Load(path string) (*Registry, error) {
	registry := &Registry{path: path, Sources: []Source{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return registry, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read environment sources: %w", err)
	}
	if err := json.Unmarshal(data, registry); err != nil {
		return nil, fmt.Errorf("parse environment sources: %w", err)
	}
	registry.path = path
	if err := registry.validateSnapshot(); err != nil {
		return nil, err
	}
	registry.sort()
	return registry, nil
}

func (r *Registry) List() []Source {
	out := append([]Source(nil), r.Sources...)
	return out
}

func (r *Registry) Get(name string) (Source, error) {
	for _, source := range r.Sources {
		if source.Name == name {
			return source, nil
		}
	}
	return Source{}, fmt.Errorf("%w: %s", ErrNotFound, name)
}

func (r *Registry) Default() (Source, error) {
	for _, source := range r.Sources {
		if source.Default {
			return source, nil
		}
	}
	return Source{}, ErrNotFound
}

func (r *Registry) Add(source Source, makeDefault bool) error {
	if err := source.Validate(); err != nil {
		return err
	}
	if _, err := r.Get(source.Name); !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("environment source %q already exists", source.Name)
	}
	if len(r.Sources) == 0 || makeDefault {
		r.clearDefault()
		source.Default = true
	} else {
		source.Default = false
	}
	previous := append([]Source(nil), r.Sources...)
	r.Sources = append(r.Sources, source)
	r.sort()
	if err := r.save(); err != nil {
		r.Sources = previous
		return err
	}
	return nil
}

func (r *Registry) Replace(source Source) error {
	if err := source.Validate(); err != nil {
		return err
	}
	for i := range r.Sources {
		if r.Sources[i].Name == source.Name {
			previous := append([]Source(nil), r.Sources...)
			source.Default = r.Sources[i].Default
			r.Sources[i] = source
			r.sort()
			if err := r.save(); err != nil {
				r.Sources = previous
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNotFound, source.Name)
}

func (r *Registry) SetDefault(name string) error {
	if _, err := r.Get(name); err != nil {
		return err
	}
	previous := append([]Source(nil), r.Sources...)
	for i := range r.Sources {
		r.Sources[i].Default = r.Sources[i].Name == name
	}
	if err := r.save(); err != nil {
		r.Sources = previous
		return err
	}
	return nil
}

func (r *Registry) Remove(name string) (Source, error) {
	for i, source := range r.Sources {
		if source.Name != name {
			continue
		}
		if source.Default && len(r.Sources) > 1 {
			return Source{}, fmt.Errorf("environment source %q is default; set another default before removing it", name)
		}
		previous := append([]Source(nil), r.Sources...)
		r.Sources = append(r.Sources[:i], r.Sources[i+1:]...)
		if err := r.save(); err != nil {
			r.Sources = previous
			return Source{}, err
		}
		return source, nil
	}
	return Source{}, fmt.Errorf("%w: %s", ErrNotFound, name)
}

func (r *Registry) validateSnapshot() error {
	seen := map[string]bool{}
	defaults := 0
	for _, source := range r.Sources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("invalid environment source: %w", err)
		}
		if seen[source.Name] {
			return fmt.Errorf("duplicate environment source %q", source.Name)
		}
		seen[source.Name] = true
		if source.Default {
			defaults++
		}
	}
	if len(r.Sources) > 0 && defaults != 1 {
		return fmt.Errorf("environment source registry has %d defaults; want exactly one", defaults)
	}
	return nil
}

func (r *Registry) clearDefault() {
	for i := range r.Sources {
		r.Sources[i].Default = false
	}
}

func (r *Registry) sort() {
	sort.Slice(r.Sources, func(i, j int) bool {
		if r.Sources[i].Default != r.Sources[j].Default {
			return r.Sources[i].Default
		}
		return r.Sources[i].Name < r.Sources[j].Name
	})
}

func (r *Registry) save() error {
	if err := r.validateSnapshot(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode environment sources: %w", err)
	}
	if err := atomicio.WriteFile(r.path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write environment sources: %w", err)
	}
	return nil
}

func RegistryPath(orbitHome string) string {
	return filepath.Join(orbitHome, "sources.json")
}

func CacheDir(orbitHome, sourceName string) string {
	return filepath.Join(SourceDir(orbitHome, sourceName), "current")
}

func SourceDir(orbitHome, sourceName string) string {
	return filepath.Join(orbitHome, "sources", sourceName)
}

func ContainsPath(orbitHome, sourceName, path string) bool {
	if path == "" {
		return false
	}
	directory := canonicalPath(EnvsDir(orbitHome, sourceName))
	path = canonicalPath(path)
	relative, err := filepath.Rel(directory, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func canonicalPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

func (r *Registry) SourceForPath(orbitHome, path string) (Source, string, bool) {
	for _, source := range r.List() {
		if ContainsPath(orbitHome, source.Name, path) {
			return source, Identity(source.Name, filepath.Base(path)), true
		}
	}
	return Source{}, "", false
}

func EnvsDir(orbitHome, sourceName string) string {
	return filepath.Join(CacheDir(orbitHome, sourceName), "envs")
}

func Identity(sourceName, environmentName string) string {
	return sourceName + "/" + strings.TrimSuffix(environmentName, filepath.Ext(environmentName))
}

func ParseIdentity(identity string) (string, string, error) {
	if strings.Count(identity, "/") != 1 || strings.Contains(identity, `\`) {
		return "", "", fmt.Errorf("managed environment identity must be <source>/<environment>: %q", identity)
	}
	parts := strings.SplitN(identity, "/", 2)
	if !namePattern.MatchString(parts[0]) || !namePattern.MatchString(parts[1]) || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return "", "", fmt.Errorf("invalid managed environment identity %q", identity)
	}
	return parts[0], strings.TrimSuffix(parts[1], filepath.Ext(parts[1])), nil
}
