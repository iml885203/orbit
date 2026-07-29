package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level orbit.yaml structure.
type Config struct {
	Version string `yaml:"version"`
	// PreviewOnly marks the env as inspectable but not activatable on
	// this machine. The daemon rejects switch attempts (CLI and UI
	// both go through the same endpoint, so both paths are covered).
	// Defends the active machine from misclicks; not a security
	// boundary — a user with write access to the yaml can flip it.
	PreviewOnly bool                  `yaml:"previewOnly"`
	Settings    RuntimeSettings       `yaml:"settings"`
	Groups      map[string]Group      `yaml:"groups"`
	Containers  map[string]*Container `yaml:"containers"`
	Services    map[string]*Service   `yaml:"services"`
	Externals   map[string]*External  `yaml:"externals"`
	// Tracing is the local OpenTelemetry section. When on, the daemon runs an
	// OTLP/HTTP receiver and injects OTEL_* env into dev services so their
	// spans flow into Orbit. Tracing is on unless the environment explicitly
	// sets enabled: false. See Config.TracingEnabled.
	Tracing *TracingConfig `yaml:"tracing"`
	// Extensions collects top-level keys owned by registered extension
	// sections (e.g. "claim"). yaml:",inline" routes unknown top-level
	// keys here INSTEAD of tripping KnownFields (yaml.v3 behavior), so
	// Load enforces the registered-section allowlist itself — an
	// unregistered key still fails loudly. Raw nodes only; readers use
	// Config.Extension(name) for the decoded value. Hidden from codegen
	// and the wire (json:"-").
	Extensions map[string]yaml.Node `yaml:",inline" json:"-"`
	// ext holds the decoded section values, written once by Load via the
	// registered decoders — immutable afterwards like the rest of a
	// published Config.
	ext map[string]any
}

// TracingConfig configures the built-in local OTLP receiver. The receiver is
// HTTP-only (OTLP/HTTP on OTLPPort); services are pointed at it by injecting
// OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf. Spans live in an in-memory ring
// buffer of at most MaxTraces traces and are dropped on `orbit down`.
type TracingConfig struct {
	Enabled   *bool `yaml:"enabled"`
	OTLPPort  int   `yaml:"otlp_port"`  // OTLP/HTTP receiver port; default 4318
	MaxTraces int   `yaml:"max_traces"` // ring-buffer capacity in traces; default 1000
}

const (
	defaultOTLPPort  = 4318
	defaultMaxTraces = 1000
)

// TracingEnabled reports whether local tracing is on for this env.
//
// Tracing follows one opt-out rule: absent configuration and a section used
// only to tune the receiver both keep it on. Only enabled: false turns it off.
func (c *Config) TracingEnabled() bool {
	return c.Tracing == nil || c.Tracing.Enabled == nil || *c.Tracing.Enabled
}

// TracingOTLPPort returns the configured OTLP/HTTP port or the default.
func (c *Config) TracingOTLPPort() int {
	if c.Tracing != nil && c.Tracing.OTLPPort > 0 {
		return c.Tracing.OTLPPort
	}
	return defaultOTLPPort
}

// TracingPortExplicit reports whether the env pinned an OTLP port. When true,
// the daemon binds exactly that port and does NOT fall back on conflict (the
// user asked for a specific port — silently moving would hide a real clash).
// When false the port is the implicit default and the daemon may auto-advance
// past a conflict. See ListenAndServe's OTLP bind.
func (c *Config) TracingPortExplicit() bool {
	return c.Tracing != nil && c.Tracing.OTLPPort > 0
}

// TracingMaxTraces returns the configured ring-buffer capacity or the default.
func (c *Config) TracingMaxTraces() int {
	if c.Tracing != nil && c.Tracing.MaxTraces > 0 {
		return c.Tracing.MaxTraces
	}
	return defaultMaxTraces
}

type RuntimeSettings struct {
	ShutdownTimeout     time.Duration `yaml:"shutdown_timeout"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`
	DockerPollInterval  time.Duration `yaml:"docker_poll_interval"`
}

// Group is a named collection of services. It serves two purposes:
//  1. Startup batching — `orbit up --groups X` brings up only the
//     services listed under group X.
//  2. Visual clustering on the dashboard (preview-only envs draw each
//     group as a labeled box around its services).
//
// Color is a free-form CSS color (e.g. "#d97706"). When empty the UI
// derives a stable hue from the group name.
type Group struct {
	Enabled  bool     `yaml:"enabled"`
	Color    string   `yaml:"color,omitempty"`
	Services []string `yaml:"services"`
}

// KafkaIO is the optional kafka section on services and externals.
// Listed topics are literal strings — no pattern matching. produces
// and consumes are independent: a service can produce, consume, both,
// or neither.
type KafkaIO struct {
	Produces []string `yaml:"produces" json:"produces"`
	Consumes []string `yaml:"consumes" json:"consumes"`
}

// External is an out-of-workspace producer/consumer (e.g. an upstream
// feed, a 3rd-party payment provider). It is rendered as a placeholder
// node so async edges to/from external systems are visible on the
// canvas. Externals never participate in startup; the daemon never
// tries to launch them.
type External struct {
	Name  string  `yaml:"-"` // populated after load
	Label string  `yaml:"label"`
	Color string  `yaml:"color,omitempty"`
	Kafka KafkaIO `yaml:"kafka"`
}

type Container struct {
	Name        string             `yaml:"-"` // populated after load
	Image       string             `yaml:"image"`
	Icon        string             `yaml:"icon"`
	PullPolicy  string             `yaml:"pull_policy"` // always, if_not_present, never
	Platform    string             `yaml:"platform"`    // e.g. linux/amd64
	Ports       map[string]PortDef `yaml:"ports"`
	Environment map[string]string  `yaml:"environment"`
	Volumes     []string           `yaml:"volumes"`
	Command     []string           `yaml:"command"`
	User        string             `yaml:"user"` // docker --user, e.g. "0:0"; needed by images that require root on a fresh named volume
	Entrypoint  []string           `yaml:"entrypoint"`
	HealthCheck *HealthCheckConfig `yaml:"health_check"`
	Init        *InitConfig        `yaml:"init"`
	Seed        *SeedConfig        `yaml:"seed"`
	Sidecars    []Sidecar          `yaml:"sidecars"`
	DependsOn   []string           `yaml:"depends_on"`
	Kind        string             `yaml:"kind"` // frontend | backend | infra
}

// WithContainer returns a copy of c whose Containers map has name
// replaced by ctr. The copy is shallow apart from the fresh Containers
// map: every other field aliases c, which is safe because published
// configs are immutable. This is the splice restartSQLServer needs —
// deliberately NOT a full reload, so edits elsewhere in the env file
// don't ride along outside an explicit env switch.
func (c *Config) WithContainer(name string, ctr *Container) *Config {
	next := *c
	next.Containers = make(map[string]*Container, len(c.Containers)+1)
	for k, v := range c.Containers {
		next.Containers[k] = v
	}
	next.Containers[name] = ctr
	return &next
}

// PortDef supports both simple (6379) and mapped (8989:8080) port definitions.
// YAML: `redis: 6379` or `ui: "8989:8080"` (host:container)
type PortDef struct {
	Host      int
	Target    int // container-internal port; same as Host if not specified
	auto      bool
	preferred int
}

func (p PortDef) IsAuto() bool {
	return p.auto
}

func (p PortDef) PreferredHost() int {
	if p.auto && p.preferred > 0 {
		return p.preferred
	}
	return p.Host
}

func (p *PortDef) UnmarshalYAML(unmarshal func(any) error) error {
	// Try int first: `redis: 6379`
	var single int
	if err := unmarshal(&single); err == nil {
		p.Host = single
		p.Target = single
		return nil
	}
	// Try string: `ui: "8989:8080"`
	var s string
	if err := unmarshal(&s); err == nil {
		return p.parseMapping(s)
	}
	return fmt.Errorf("port must be int or \"host:target\" string")
}

func (p *PortDef) parseMapping(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("port mapping is empty")
	}
	parts := strings.SplitN(s, ":", 2)
	host, err := parsePort(parts[0])
	if err != nil {
		return fmt.Errorf("invalid port mapping %q: host %w", s, err)
	}
	p.Host = host
	if len(parts) == 2 {
		target, err := parsePort(parts[1])
		if err != nil {
			return fmt.Errorf("invalid port mapping %q: target %w", s, err)
		}
		p.Target = target
	} else {
		p.Target = host
	}
	return nil
}

// parsePort validates a port string in range 1-65535.
func parsePort(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("port is empty")
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("port %q is not a number", s)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("port %d out of range (1-65535)", n)
	}
	return n, nil
}

type Sidecar struct {
	Name        string             `yaml:"name"`
	Image       string             `yaml:"image"`
	PullPolicy  string             `yaml:"pull_policy"`
	Ports       map[string]PortDef `yaml:"ports"`
	Environment map[string]string  `yaml:"environment"`
	Volumes     []string           `yaml:"volumes"`
	DependsOn   []string           `yaml:"depends_on"`
}

type SeedConfig struct {
	Type        string   `yaml:"type"`         // sqlserver or mongo
	Database    string   `yaml:"database"`     // target database (MongoDB)
	Username    string   `yaml:"username"`     // SQL Server login
	PasswordEnv string   `yaml:"password_env"` // SQL Server container env key
	Files       []string `yaml:"files"`        // seed file paths, executed in order
}

type InitConfig struct {
	Type       string   `yaml:"type"` // kafka_topics, mongo_rs
	TopicsFile string   `yaml:"topics_file"`
	RSMembers  []string `yaml:"rs_members"`
}

type Service struct {
	Name        string               `yaml:"-"`    // populated after load
	Type        string               `yaml:"type"` // dotnet, node
	Path        string               `yaml:"path"`
	Command     string               `yaml:"command"`
	Watch       bool                 `yaml:"watch"` // use file watcher (dotnet watch), default false
	URL         string               `yaml:"url"`   // entry point URL for orbit open
	Ports       map[string]PortDef   `yaml:"ports"`
	Env         map[string]string    `yaml:"env"`
	BuildEnv    map[string]string    `yaml:"build_env"` // env vars passed to the build step only (e.g. dotnet build), not to the running process
	EnvToggles  map[string]EnvToggle `yaml:"env_toggles"`
	HealthCheck *HealthCheckConfig   `yaml:"health_check"`
	DependsOn   []string             `yaml:"depends_on"`
	PreStart    []string             `yaml:"pre_start"`
	Kind        string               `yaml:"kind"` // frontend | backend | infra
	Kafka       KafkaIO              `yaml:"kafka"`
}

// EnvToggle defines a toggleable environment variable.
type EnvToggle struct {
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
	Default     bool   `yaml:"default"`
}

// GetDependencies returns the depends_on list for a service or container by name.
func (c *Config) GetDependencies(name string) []string {
	if svc, ok := c.Services[name]; ok {
		return svc.DependsOn
	}
	if ctr, ok := c.Containers[name]; ok {
		return ctr.DependsOn
	}
	return nil
}

// ServiceOrContainerExists returns true if name is a known service or container.
func (c *Config) ServiceOrContainerExists(name string) bool {
	if _, ok := c.Services[name]; ok {
		return true
	}
	_, ok := c.Containers[name]
	return ok
}

type HealthCheckConfig struct {
	Type             string        `yaml:"type"` // http, tcp, log, exec, healthcheck
	Path             string        `yaml:"path"`
	Port             int           `yaml:"port"`
	Pattern          string        `yaml:"pattern"` // regex for "log" type
	Command          []string      `yaml:"command"` // argv for "exec" type
	Interval         time.Duration `yaml:"interval"`
	Timeout          time.Duration `yaml:"timeout"`
	Retries          int           `yaml:"retries"`
	FailureThreshold int           `yaml:"failure_threshold"`
}

// validKinds is the closed set of allowed kind values.
var validKinds = map[string]bool{"frontend": true, "backend": true, "infra": true}

// ResolveKind returns the explicit kind or the fallback for a service ("backend").
func (s *Service) ResolveKind() string {
	if validKinds[s.Kind] {
		return s.Kind
	}
	return "backend"
}

// ResolveKind returns the explicit kind or the fallback for a container ("infra").
func (c *Container) ResolveKind() string {
	if validKinds[c.Kind] {
		return c.Kind
	}
	return "infra"
}

// supportedEnvVersion is the only env schema version this binary accepts.
// Bump this in lockstep with any breaking change to the env YAML structure.
//
// v1 → v2: `features:` renamed to `groups:`; `Feature` struct renamed to
//
//	`Group`; new optional `color` field on each group.
const supportedEnvVersion = "2"

type SchemaVersionMismatchKind string

const (
	SchemaVersionMissing SchemaVersionMismatchKind = "missing"
	SchemaVersionOlder   SchemaVersionMismatchKind = "older"
	SchemaVersionNewer   SchemaVersionMismatchKind = "newer"
	SchemaVersionInvalid SchemaVersionMismatchKind = "invalid"
)

type SchemaVersionMismatchError struct {
	Path      string
	Found     string
	Supported string
	Kind      SchemaVersionMismatchKind
}

func (e *SchemaVersionMismatchError) Error() string {
	switch e.Kind {
	case SchemaVersionMissing:
		return fmt.Sprintf(
			"env file %s is missing required field 'version' (expected %q). "+
				"This file may be from an older Orbit that didn't require versioning, or it may be corrupt",
			e.Path, e.Supported,
		)
	case SchemaVersionOlder:
		return fmt.Sprintf(
			"env file %s: schema version %q but this Orbit binary requires %q. "+
				"Your env files are out of date — pull the latest env revision or run 'orbit env sync'",
			e.Path, e.Found, e.Supported,
		)
	case SchemaVersionNewer:
		return fmt.Sprintf(
			"env file %s: schema version %q but this Orbit binary supports %q. "+
				"Your Orbit binary is out of date — run 'orbit update' or check out an older env revision",
			e.Path, e.Found, e.Supported,
		)
	default:
		return fmt.Sprintf(
			"env file %s: unrecognized schema version %q (expected %q)",
			e.Path, e.Found, e.Supported,
		)
	}
}

// CheckVersion returns an error if version doesn't match supportedEnvVersion.
// path is included in the error to identify which env file is mismatched.
// When both versions parse as integers, the error message distinguishes
// "env older than binary" from "env newer than binary" so the user knows
// which side to upgrade.
func CheckVersion(version, path string) error {
	if version == supportedEnvVersion {
		return nil
	}
	if version == "" {
		return &SchemaVersionMismatchError{
			Path:      path,
			Supported: supportedEnvVersion,
			Kind:      SchemaVersionMissing,
		}
	}
	envN, envErr := strconv.Atoi(version)
	binN, binErr := strconv.Atoi(supportedEnvVersion)
	if envErr == nil && binErr == nil {
		if envN < binN {
			return &SchemaVersionMismatchError{
				Path:      path,
				Found:     version,
				Supported: supportedEnvVersion,
				Kind:      SchemaVersionOlder,
			}
		}
		return &SchemaVersionMismatchError{
			Path:      path,
			Found:     version,
			Supported: supportedEnvVersion,
			Kind:      SchemaVersionNewer,
		}
	}
	return &SchemaVersionMismatchError{
		Path:      path,
		Found:     version,
		Supported: supportedEnvVersion,
		Kind:      SchemaVersionInvalid,
	}
}
