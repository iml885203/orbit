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
version: "2"
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
			version: "2",
			path:    "envs/development.yaml",
			wantErr: false,
		},
		{
			name:        "env older than binary",
			version:     "1",
			path:        "envs/development.yaml",
			wantErr:     true,
			wantContain: []string{"envs/development.yaml", `"1"`, `"2"`, "out of date", "env sync"},
		},
		{
			name:        "env newer than binary",
			version:     "3",
			path:        "envs/development.yaml",
			wantErr:     true,
			wantContain: []string{"envs/development.yaml", `"3"`, `"2"`, "orbit binary is out of date", "orbit upgrade"},
		},
		{
			name:        "missing version",
			version:     "",
			path:        "envs/development.yaml",
			wantErr:     true,
			wantContain: []string{"envs/development.yaml", "missing required field 'version'", `"2"`},
		},
		{
			name:        "non-numeric version",
			version:     "beta",
			path:        "envs/development.yaml",
			wantErr:     true,
			wantContain: []string{"envs/development.yaml", `"beta"`, `"2"`},
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

func TestValidate_SQLServerRequiresSAPassword(t *testing.T) {
	cfg := &Config{
		Containers: map[string]*Container{"sql-server": {Name: "sql-server"}},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for sql-server without SA_PASSWORD")
	} else if !strings.Contains(err.Error(), "SA_PASSWORD") {
		t.Errorf("error %q must mention 'SA_PASSWORD'", err)
	}

	cfg.Containers["sql-server"].Environment = map[string]string{"SA_PASSWORD": "pw"}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected valid config with SA_PASSWORD set, got %v", err)
	}

	// Image-sniffed MSSQL containers are held to the same requirement —
	// env injection and query sql treat them as SQL Server too.
	sniffed := &Config{Containers: map[string]*Container{
		"legacy-db": {Name: "legacy-db", Image: "mcr.microsoft.com/mssql/server:2022-latest"},
	}}
	if err := Validate(sniffed); err == nil {
		t.Error("expected error for mssql-image container without SA_PASSWORD")
	}

	// Non-SQL containers stay unconstrained.
	other := &Config{Containers: map[string]*Container{"postgres": {Name: "postgres", Image: "postgres:16"}}}
	if err := Validate(other); err != nil {
		t.Errorf("expected non-sql-server container to pass, got %v", err)
	}
}

func TestValidate_SQLProjectsTargetOnly(t *testing.T) {
	// A sql_projects section may declare just the target and rely on
	// discovery — projects is optional (it moved out of the env).
	cfg := &Config{
		Containers: map[string]*Container{
			"sql-server": {Name: "sql-server", Environment: map[string]string{"SA_PASSWORD": "pw"}},
		},
		SQLProjects: &SQLProjectsConfig{Target: "sql-server"},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("target-only sql_projects should be valid, got %v", err)
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

func TestConfig_TracingThreeState(t *testing.T) {
	// Absent section → auto-on (Aspire-aligned default).
	absent := &Config{}
	if !absent.TracingEnabled() {
		t.Error("absent tracing section should be enabled (default-on)")
	}

	// Explicit enabled: false → opt-out.
	off := &Config{Tracing: &TracingConfig{Enabled: false}}
	if off.TracingEnabled() {
		t.Error("explicit enabled:false should be disabled")
	}

	// Explicit enabled: true → on.
	on := &Config{Tracing: &TracingConfig{Enabled: true}}
	if !on.TracingEnabled() {
		t.Error("explicit enabled:true should be enabled")
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
