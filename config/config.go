package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/iml885203/orbit/volume"
	"gopkg.in/yaml.v3"
)

// envVarPattern matches the innermost ${...} — content has no "${" — so
// repeated application unwraps nested expressions like
// ${OUTER:-${INNER:-default}} from the inside out.
var envVarPattern = regexp.MustCompile(`\$\{((?:[^${}]|\$[^{])+)\}`)

// Load reads orbit.yaml from path, substitutes env vars, and validates.
func Load(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("no env selected — run 'orbit init', 'orbit switch <env>', or pass --config")
	}
	_, expanded, err := loadInheritedDocument(path)
	if err != nil {
		return nil, err
	}

	var header struct {
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal([]byte(expanded), &header); err != nil {
		return nil, fmt.Errorf("parsing env file %s: %w", path, err)
	}
	if err := CheckVersion(header.Version, path); err != nil {
		return nil, err
	}

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(expanded))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing env file %s: %w", path, addSchemaFieldGuidance(err, &cfg))
	}

	// Registered extension sections (allowlist + decoders): the inline
	// Extensions map absorbed every non-core top-level key, so this is
	// where unknown keys fail and where feature sections (e.g. claim)
	// decode and validate.
	if err := decodeExtensionSections(&cfg, path); err != nil {
		return nil, fmt.Errorf("parsing env file %s: %w", path, err)
	}

	applyDefaults(&cfg)
	populateNames(&cfg)
	applyPathResolution(&cfg, path)

	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// applyPathResolution normalizes every path-valued field in cfg by first
// expanding leading "~/" segments to the user's home directory, then
// resolving any still-relative paths against the config file's directory.
// The order matters: "~/foo.yaml" must become "/home/user/foo.yaml" before
// the relative-path resolver runs, otherwise it would be joined to baseDir
// as "baseDir/~/foo.yaml".
func applyPathResolution(cfg *Config, cfgPath string) {
	expandHomePaths(cfg)
	resolveRelativePaths(cfg, cfgPath)
}

// resolveRelativePaths rewrites path-valued fields inside cfg so they are
// absolute, interpreted relative to the config file's directory. This lets
// an env yaml live at ~/.orbit/envs/example.yaml and still reference
// ./data/kafka-topics.yaml next to it.
func resolveRelativePaths(cfg *Config, cfgPath string) {
	absCfg, err := filepath.Abs(cfgPath)
	if err != nil {
		return
	}
	baseDir := filepath.Dir(absCfg)
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(baseDir, p)
	}
	resolveVolumes := func(volumes []string) {
		for i, value := range volumes {
			source, suffix := volume.SplitShort(value)
			if volume.IsRelativeBindSource(source) {
				volumes[i] = resolve(source) + suffix
			}
		}
	}
	for _, s := range cfg.Services {
		s.Path = resolve(s.Path)
	}
	for _, c := range cfg.Containers {
		resolveVolumes(c.Volumes)
		if c.Init != nil {
			c.Init.TopicsFile = resolve(c.Init.TopicsFile)
		}
		if c.Seed != nil {
			for i, f := range c.Seed.Files {
				c.Seed.Files[i] = resolve(f)
			}
		}
		for i := range c.Sidecars {
			resolveVolumes(c.Sidecars[i].Volumes)
		}
	}
}

// expandHome replaces a leading "~/" (or bare "~") with the user's home
// directory. Other forms (e.g. "~user/") are left untouched — matching the
// subset shells expand unambiguously without parsing /etc/passwd. Yaml
// authors expect "~/dev/foo" to behave like in the shell; without this,
// the literal "~" reaches os.Chdir/exec.Dir and fails with a confusing
// "no such file or directory" error.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// expandHomeVolume expands a leading "~/" in the host side of a
// "host:container[:mode]" volume string, leaving the container path untouched.
// Running the whole string through expandHome would filepath.Join the tail and
// flip the container path's "/" to "\" on Windows (e.g.
// "~/db:/var/lib/pg" -> "C:\Users\me\db:\var\lib\pg"), breaking the mount — the
// container path is always a Linux path.
func expandHomeVolume(v string) string {
	if v != "~" && !strings.HasPrefix(v, "~/") {
		return v
	}
	host, suffix := volume.SplitShort(v)
	return expandHome(host) + suffix
}

// expandHomePaths walks every path-valued field in cfg and rewrites leading
// "~/" segments using the current user's home directory.
func expandHomePaths(cfg *Config) {
	expandVolumes := func(s []string) {
		for i, v := range s {
			s[i] = expandHomeVolume(v)
		}
	}
	for _, s := range cfg.Services {
		s.Path = expandHome(s.Path)
	}
	for _, c := range cfg.Containers {
		expandVolumes(c.Volumes)
		if c.Init != nil {
			c.Init.TopicsFile = expandHome(c.Init.TopicsFile)
		}
		if c.Seed != nil {
			for i, f := range c.Seed.Files {
				c.Seed.Files[i] = expandHome(f)
			}
		}
		for i := range c.Sidecars {
			expandVolumes(c.Sidecars[i].Volumes)
		}
	}
}

func substituteEnvVars(input string) string {
	// Repeated passes from the inside out: each pass replaces the
	// innermost ${...} occurrences; nested expressions become eligible
	// once their inner siblings collapse. Bounded by a max iteration
	// to avoid runaway loops on pathological input.
	const maxPasses = 16
	for i := 0; i < maxPasses; i++ {
		next := envVarPattern.ReplaceAllStringFunc(input, expandOne)
		if next == input {
			return input
		}
		input = next
	}
	return input
}

func expandOne(match string) string {
	parts := envVarPattern.FindStringSubmatch(match)
	if len(parts) < 2 {
		return match
	}
	varExpr := parts[1]
	if idx := strings.Index(varExpr, ":-"); idx != -1 {
		name := varExpr[:idx]
		def := varExpr[idx+2:]
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		return def
	}
	if val, ok := os.LookupEnv(varExpr); ok {
		return val
	}
	return match
}

// DefaultHealthRetries sizes the startup probe budget at roughly one minute
// with the default 5s interval. The old default of 3 (≈15s) was routinely
// spent before a source-run service finished its first-request warm-up. For
// http/tcp checks exhaustion is no longer terminal either way: health recovery
// probing keeps watching after the budget runs out. Exported because the
// health checker's fallback shares it.
const DefaultHealthRetries = 12

// DefaultHealthFailureThreshold avoids turning one transient runtime probe
// into a red environment while still surfacing a real outage quickly.
const DefaultHealthFailureThreshold = 3

func applyDefaults(cfg *Config) {
	if cfg.Settings.ShutdownTimeout == 0 {
		cfg.Settings.ShutdownTimeout = 30 * time.Second
	}
	if cfg.Settings.HealthCheckInterval == 0 {
		cfg.Settings.HealthCheckInterval = 5 * time.Second
	}
	if cfg.Settings.DockerPollInterval == 0 {
		cfg.Settings.DockerPollInterval = 2 * time.Second
	}

	for _, c := range cfg.Containers {
		if c.PullPolicy == "" {
			c.PullPolicy = "always"
		}
		applyHealthCheckDefaults(c.HealthCheck, c.Ports, cfg.Settings.HealthCheckInterval)
	}

	for _, s := range cfg.Services {
		if s.Path == "" {
			s.Path = "."
		}
		if s.Type == "" {
			s.Type = inferServiceType(s.Command)
		}
		applyHealthCheckDefaults(s.HealthCheck, s.Ports, cfg.Settings.HealthCheckInterval)
		if s.Type == "dotnet" && s.Command == "" {
			s.Command = "dotnet watch run"
		}
	}
}

func inferServiceType(command string) string {
	fields := strings.Fields(command)
	for len(fields) > 0 && strings.Contains(fields[0], "=") {
		fields = fields[1:]
	}
	if len(fields) > 1 && fields[0] == "env" {
		fields = fields[1:]
		for len(fields) > 0 && strings.Contains(fields[0], "=") {
			fields = fields[1:]
		}
	}
	if len(fields) == 0 {
		return ""
	}
	executable := strings.ToLower(filepath.Base(fields[0]))
	switch {
	case strings.HasPrefix(executable, "python"), executable == "uv", executable == "poetry":
		return "python"
	case executable == "node", executable == "npm", executable == "npx",
		executable == "pnpm", executable == "yarn", executable == "bun":
		return "node"
	case executable == "go":
		return "go"
	default:
		return ""
	}
}

// applyHealthCheckDefaults fills the zero fields of a health_check stanza.
// Shared by the container and service loops so the two can't drift.
func applyHealthCheckDefaults(
	hc *HealthCheckConfig,
	ports map[string]PortDef,
	interval time.Duration,
) {
	if hc == nil {
		return
	}
	if hc.Type == "http" && hc.Scheme == "" {
		hc.Scheme = "http"
	}
	if hc.Port == 0 && (hc.Type == "http" || hc.Type == "tcp") {
		if hc.Type == "http" {
			if port, ok := ports[hc.Scheme]; ok {
				hc.Port = port.Host
			}
		}
		if hc.Port == 0 && len(ports) == 1 {
			for _, port := range ports {
				hc.Port = port.Host
			}
		}
	}
	if hc.Interval == 0 {
		hc.Interval = interval
	}
	if hc.Timeout == 0 {
		hc.Timeout = 5 * time.Second
	}
	if hc.Retries == 0 {
		hc.Retries = DefaultHealthRetries
	}
	if hc.FailureThreshold == 0 {
		hc.FailureThreshold = DefaultHealthFailureThreshold
	}
}

func populateNames(cfg *Config) {
	for name, c := range cfg.Containers {
		c.Name = name
	}
	for name, s := range cfg.Services {
		s.Name = name
	}
	for name, ext := range cfg.Externals {
		if ext == nil {
			continue
		}
		ext.Name = name
	}
}
