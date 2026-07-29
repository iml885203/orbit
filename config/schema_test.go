package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseMapping_SingleValid(t *testing.T) {
	var p PortDef
	if err := p.parseMapping("8080"); err != nil {
		t.Fatal(err)
	}
	if p.Host != 8080 || p.Target != 8080 {
		t.Errorf("got host=%d target=%d, want 8080/8080", p.Host, p.Target)
	}
}

func TestParseMapping_HostAndTarget(t *testing.T) {
	var p PortDef
	if err := p.parseMapping("8989:8080"); err != nil {
		t.Fatal(err)
	}
	if p.Host != 8989 || p.Target != 8080 {
		t.Errorf("got host=%d target=%d, want 8989/8080", p.Host, p.Target)
	}
}

func TestParseMapping_Whitespace(t *testing.T) {
	var p PortDef
	if err := p.parseMapping("  8080  "); err != nil {
		t.Fatal(err)
	}
	if p.Host != 8080 {
		t.Errorf("trim failed: host=%d", p.Host)
	}
}

func TestService_ResolveKind(t *testing.T) {
	tests := []struct {
		name string
		svc  Service
		want string
	}{
		{"explicit frontend", Service{Kind: "frontend"}, "frontend"},
		{"explicit backend", Service{Kind: "backend"}, "backend"},
		{"explicit infra", Service{Kind: "infra"}, "infra"},
		{"empty falls back to backend", Service{}, "backend"},
		{"unknown falls back to backend", Service{Kind: "weird"}, "backend"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.svc.ResolveKind(); got != tc.want {
				t.Errorf("ResolveKind() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestContainer_ResolveKind(t *testing.T) {
	tests := []struct {
		name string
		ctr  Container
		want string
	}{
		{"explicit infra", Container{Kind: "infra"}, "infra"},
		{"empty falls back to infra", Container{}, "infra"},
		{"unknown falls back to infra", Container{Kind: "weird"}, "infra"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ctr.ResolveKind(); got != tc.want {
				t.Errorf("ResolveKind() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestService_KindYAMLParse(t *testing.T) {
	yamlBlob := []byte("kind: frontend\ntype: node\n")
	var s Service
	if err := yaml.Unmarshal(yamlBlob, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Kind != "frontend" {
		t.Errorf("Kind = %q, want frontend", s.Kind)
	}
}

func TestParseMapping_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
		match string // substring expected in error
	}{
		{"empty", "", "empty"},
		{"whitespace", "   ", "empty"},
		{"trailing colon", "8080:", "target"},
		{"leading colon", ":8080", "host"},
		{"not numeric", "abc", "not a number"},
		{"target not numeric", "8080:abc", "not a number"},
		{"zero", "0", "out of range"},
		{"negative", "-1", "out of range"},
		{"too big", "70000", "out of range"},
		{"target out of range", "8080:99999", "out of range"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p PortDef
			err := p.parseMapping(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.input)
			}
			if !strings.Contains(err.Error(), tc.match) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.match)
			}
		})
	}
}

func TestService_KafkaUnmarshal(t *testing.T) {
	yamlSrc := `
type: dotnet
kafka:
  produces: [accounts.settlement, promotions.sports.settle]
  consumes: [single-bet-source, mp-bet-source]
`
	var svc Service
	if err := yaml.Unmarshal([]byte(yamlSrc), &svc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := svc.Kafka.Produces; len(got) != 2 || got[0] != "accounts.settlement" {
		t.Errorf("Produces = %v, want 2 entries starting with accounts.settlement", got)
	}
	if got := svc.Kafka.Consumes; len(got) != 2 || got[1] != "mp-bet-source" {
		t.Errorf("Consumes = %v, want 2 entries ending with mp-bet-source", got)
	}
}

func TestConfig_ExternalsUnmarshal(t *testing.T) {
	yamlSrc := `
version: "3"
externals:
  upstream:
    label: Upstream
    color: "#5b21b6"
    kafka:
      produces: [upstream.pricing.odds]
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlSrc), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ext, ok := cfg.Externals["upstream"]
	if !ok {
		t.Fatal("externals[upstream] missing")
	}
	if ext.Label != "Upstream" || ext.Color != "#5b21b6" {
		t.Errorf("label/color = %q/%q, want Upstream/#5b21b6", ext.Label, ext.Color)
	}
	if len(ext.Kafka.Produces) != 1 || ext.Kafka.Produces[0] != "upstream.pricing.odds" {
		t.Errorf("Produces = %v", ext.Kafka.Produces)
	}
}

func TestCheckVersion(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		path        string
		wantErr     bool
		wantContain []string // substrings the error must contain
	}{
		{
			name:    "matches supported",
			version: "3",
			path:    "envs/development.yaml",
			wantErr: false,
		},
		{
			name:        "env older than binary",
			version:     "2",
			path:        "envs/development.yaml",
			wantErr:     true,
			wantContain: []string{"envs/development.yaml", `"2"`, `"3"`, "supported schema", "migration guide"},
		},
		{
			name:        "env newer than binary",
			version:     "4",
			path:        "envs/development.yaml",
			wantErr:     true,
			wantContain: []string{"envs/development.yaml", `"4"`, `"3"`, "Orbit binary is out of date", "orbit update"},
		},
		{
			name:        "missing version",
			version:     "",
			path:        "envs/development.yaml",
			wantErr:     true,
			wantContain: []string{"envs/development.yaml", "missing required field 'version'", `"3"`},
		},
		{
			name:        "non-numeric version",
			version:     "beta",
			path:        "envs/development.yaml",
			wantErr:     true,
			wantContain: []string{"envs/development.yaml", `"beta"`, `"3"`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckVersion(tc.version, tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				msg := err.Error()
				for _, sub := range tc.wantContain {
					if !strings.Contains(msg, sub) {
						t.Errorf("error %q missing substring %q", msg, sub)
					}
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidate_SeedRequiresCommandAndFiles(t *testing.T) {
	tests := []struct {
		name string
		seed *SeedConfig
		want string
	}{
		{
			name: "missing command",
			seed: &SeedConfig{Files: []string{"seed.sql"}},
			want: "seed.command is required",
		},
		{
			name: "missing files",
			seed: &SeedConfig{Command: "psql -U app"},
			want: "seed.files must contain at least one file",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &Config{Containers: map[string]*Container{
				"database": {Seed: test.seed},
			}}
			err := Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}

	valid := &Config{Containers: map[string]*Container{
		"database": {
			Seed: &SeedConfig{Command: "psql -U app", Files: []string{"seed.sql"}},
		},
	}}
	if err := Validate(valid); err != nil {
		t.Fatalf("valid command seed: %v", err)
	}
}

func TestValidate_ExternalNameCollidesWithService(t *testing.T) {
	cfg := &Config{
		Services:  map[string]*Service{"upstream": {Name: "upstream"}},
		Externals: map[string]*External{"upstream": {Name: "upstream", Kafka: KafkaIO{Produces: []string{"t"}}}},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for service/external name collision")
	} else if !strings.Contains(err.Error(), "external") || !strings.Contains(err.Error(), "upstream") {
		t.Errorf("error %q must mention 'external' and 'upstream'", err)
	}
}

func TestValidate_ExternalNameCollidesWithContainer(t *testing.T) {
	cfg := &Config{
		Containers: map[string]*Container{"upstream": {Name: "upstream"}},
		Externals:  map[string]*External{"upstream": {Name: "upstream", Kafka: KafkaIO{Produces: []string{"t"}}}},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for container/external name collision")
	}
}

func TestValidate_ExternalWithoutKafka(t *testing.T) {
	cfg := &Config{
		Externals: map[string]*External{"upstream": {Name: "upstream"}},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for external without any kafka topics")
	} else if !strings.Contains(err.Error(), "kafka") {
		t.Errorf("error %q must mention 'kafka'", err)
	}
}

func TestValidate_RejectsNegativeRuntimeHealthFailureThreshold(t *testing.T) {
	cfg := &Config{
		Services: map[string]*Service{
			"api": {
				Name: "api",
				HealthCheck: &HealthCheckConfig{
					Type:             "http",
					FailureThreshold: -1,
				},
			},
		},
	}
	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "failure_threshold must be at least 1") {
		t.Fatalf("Validate() = %v, want failure_threshold error", err)
	}
}

func TestConfig_TracingUsesExplicitOptOut(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		enabled bool
	}{
		{name: "section absent", source: "version: \"3\"\n", enabled: true},
		{name: "receiver tuning only", source: "version: \"3\"\ntracing:\n  otlp_port: 5000\n", enabled: true},
		{name: "explicitly enabled", source: "version: \"3\"\ntracing:\n  enabled: true\n", enabled: true},
		{name: "explicitly disabled", source: "version: \"3\"\ntracing:\n  enabled: false\n", enabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			if err := yaml.Unmarshal([]byte(tt.source), &cfg); err != nil {
				t.Fatalf("decode config: %v", err)
			}
			if got := cfg.TracingEnabled(); got != tt.enabled {
				t.Errorf("TracingEnabled() = %t, want %t", got, tt.enabled)
			}
		})
	}
}

func TestConfig_TracingPortExplicit(t *testing.T) {
	if (&Config{}).TracingPortExplicit() {
		t.Error("absent section: port is not explicit")
	}
	if (&Config{Tracing: &TracingConfig{}}).TracingPortExplicit() {
		t.Error("section without otlp_port: port is not explicit")
	}
	if !(&Config{Tracing: &TracingConfig{OTLPPort: 5000}}).TracingPortExplicit() {
		t.Error("otlp_port set: port should be explicit")
	}
	if got := (&Config{Tracing: &TracingConfig{OTLPPort: 5000}}).TracingOTLPPort(); got != 5000 {
		t.Errorf("TracingOTLPPort = %d, want 5000", got)
	}
	if got := (&Config{}).TracingOTLPPort(); got != defaultOTLPPort {
		t.Errorf("default TracingOTLPPort = %d, want %d", got, defaultOTLPPort)
	}
}
