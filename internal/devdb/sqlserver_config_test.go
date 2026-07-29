package devdb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
)

func validSQLServerConfig() (*SQLServerConfig, *config.Config) {
	section := &SQLServerConfig{
		Target:      "database",
		Username:    "sa",
		PasswordEnv: "MSSQL_SA_PASSWORD",
		Projects: []SQLServerProjectConfig{
			{Path: "database/Accounts/Accounts.sqlproj"},
		},
	}
	cfg := &config.Config{
		Containers: map[string]*config.Container{
			"database": {
				Ports: map[string]config.PortDef{
					"sql": {Host: 14330, Target: 1433},
				},
				Environment: map[string]string{"MSSQL_SA_PASSWORD": "secret"},
			},
		},
	}
	return section, cfg
}

func TestLoadSQLServerSectionStrictAndDefaulted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dev.yaml")
	raw := []byte(`version: "3"
containers:
  database:
    image: example/sqlserver:latest
    ports:
      sql: "14330:1433"
    environment:
      DB_PASSWORD: secret
sqlserver:
  target: database
  password_env: DB_PASSWORD
  projects:
    - path: database/Accounts/Accounts.sqlproj
`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	section := SQLServerFrom(cfg)
	if section == nil {
		t.Fatal("sqlserver section missing")
	}
	if section.Username != "sa" {
		t.Fatalf("username = %q, want default sa", section.Username)
	}
	if section.PasswordEnv != "DB_PASSWORD" {
		t.Fatalf("password_env = %q", section.PasswordEnv)
	}
}

func TestValidateSQLServerSectionRequiresExplicitIntent(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SQLServerConfig, *config.Config)
		want   string
	}{
		{
			name:   "missing target",
			mutate: func(section *SQLServerConfig, _ *config.Config) { section.Target = "" },
			want:   "target is required",
		},
		{
			name:   "missing credential key",
			mutate: func(section *SQLServerConfig, _ *config.Config) { section.PasswordEnv = "" },
			want:   "password_env is required",
		},
		{
			name:   "invalid credential key",
			mutate: func(section *SQLServerConfig, _ *config.Config) { section.PasswordEnv = "BAD KEY" },
			want:   "valid environment key",
		},
		{
			name: "credential absent from target",
			mutate: func(_ *SQLServerConfig, cfg *config.Config) {
				cfg.Containers["database"].Environment = nil
			},
			want: "MSSQL_SA_PASSWORD",
		},
		{
			name: "project directory instead of file",
			mutate: func(section *SQLServerConfig, _ *config.Config) {
				section.Projects[0].Path = "database/Accounts"
			},
			want: ".sqlproj file",
		},
		{
			name: "ambiguous target port",
			mutate: func(_ *SQLServerConfig, cfg *config.Config) {
				cfg.Containers["database"].Ports = map[string]config.PortDef{
					"one": {Host: 1001, Target: 1001},
					"two": {Host: 1002, Target: 1002},
				}
			},
			want: "ambiguous",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			section, cfg := validSQLServerConfig()
			test.mutate(section, cfg)
			err := validateSQLServerSection(section, cfg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateSQLServerSectionAcceptsExplicitConfig(t *testing.T) {
	section, cfg := validSQLServerConfig()
	if err := validateSQLServerSection(section, cfg); err != nil {
		t.Fatalf("validateSQLServerSection: %v", err)
	}
}
