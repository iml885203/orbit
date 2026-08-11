package devdb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
	"gopkg.in/yaml.v3"
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

func TestInheritedSQLServerSchemaErrorKeepsSourcePathAndLine(t *testing.T) {
	tests := []struct {
		name       string
		parent     string
		child      string
		wantSource string
		wantLine   string
	}{
		{
			name:       "parent fragment",
			parent:     "version: \"3\"\n\nsqlserver:\n  target: database\n\n  typo_field: value\n",
			child:      "extends: base.yaml\n",
			wantSource: "base.yaml",
			wantLine:   "line 6: unknown field typo_field",
		},
		{
			name:       "child fragment",
			parent:     "version: \"3\"\nsqlserver:\n  target: database\n",
			child:      "extends: base.yaml\nsqlserver:\n\n  typo_field: value\n",
			wantSource: "child.yaml",
			wantLine:   "line 4: unknown field typo_field",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			parent := filepath.Join(dir, "base.yaml")
			child := filepath.Join(dir, "child.yaml")
			if err := os.WriteFile(parent, []byte(tt.parent), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(child, []byte(tt.child), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := config.Load(child)
			if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, tt.wantSource)) || !strings.Contains(err.Error(), tt.wantLine) {
				t.Fatalf("config.Load error = %v, want %s at %s", err, tt.wantLine, tt.wantSource)
			}
		})
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

func TestValidateSQLServerSectionRejectsDatabaseNamesSharedByProjects(t *testing.T) {
	section, cfg := validSQLServerConfig()
	section.Projects = append(section.Projects,
		SQLServerProjectConfig{Path: "e2e/Accounts/AccountsCopy.sqlproj", Databases: []string{"Accounts"}},
	)
	err := validateSQLServerSection(section, cfg)
	if err == nil || !strings.Contains(err.Error(), `both map database name "Accounts"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateSQLServerSectionRejectsSharedProjectNamesWithDistinctDatabases(t *testing.T) {
	section, cfg := validSQLServerConfig()
	section.Projects[0].Databases = []string{"AccountsDev"}
	section.Projects = append(section.Projects,
		SQLServerProjectConfig{Path: "e2e/Accounts/Accounts.sqlproj", Databases: []string{"AccountsE2E"}},
	)
	err := validateSQLServerSection(section, cfg)
	if err == nil || !strings.Contains(err.Error(), `both use project name "Accounts"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateSQLServerSectionRejectsExplicitEmptyDatabaseList(t *testing.T) {
	section, cfg := validSQLServerConfig()
	section.Projects[0].Databases = []string{}
	err := validateSQLServerSection(section, cfg)
	if err == nil || !strings.Contains(err.Error(), "must contain at least one database name") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateSQLServerSectionRejectsEveryExplicitEmptyDatabaseYAMLForm(t *testing.T) {
	for _, value := range []string{"[]", "", "null"} {
		t.Run("value="+value, func(t *testing.T) {
			var project SQLServerProjectConfig
			raw := "path: database/Accounts/Accounts.sqlproj\ndatabases: " + value + "\n"
			if err := yaml.Unmarshal([]byte(raw), &project); err != nil {
				t.Fatal(err)
			}
			section, cfg := validSQLServerConfig()
			section.Projects = []SQLServerProjectConfig{project}
			err := validateSQLServerSection(section, cfg)
			if err == nil || !strings.Contains(err.Error(), "must contain at least one database name") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidateSQLServerSectionDerivesDatabaseFromCleanPath(t *testing.T) {
	section, cfg := validSQLServerConfig()
	section.Projects[0].Path = "  database/Accounts/Accounts.sqlproj  "
	if err := validateSQLServerSection(section, cfg); err != nil {
		t.Fatalf("validateSQLServerSection: %v", err)
	}
	if section.Projects[0].Path != "database/Accounts/Accounts.sqlproj" ||
		len(section.Projects[0].Databases) != 1 || section.Projects[0].Databases[0] != "Accounts" {
		t.Fatalf("project = %+v", section.Projects[0])
	}
}

func TestValidateSQLServerSectionRejectsNamesSharedAcrossProjectAndDatabase(t *testing.T) {
	tests := []struct {
		name     string
		projects []SQLServerProjectConfig
	}{
		{
			name: "later database shadows earlier project",
			projects: []SQLServerProjectConfig{
				{Path: "dev/Foo/Foo.sqlproj", Databases: []string{"FooDev"}},
				{Path: "e2e/Bar/Bar.sqlproj", Databases: []string{"foo"}},
			},
		},
		{
			name: "later project is shadowed by earlier database",
			projects: []SQLServerProjectConfig{
				{Path: "dev/Foo/Foo.sqlproj", Databases: []string{"bar"}},
				{Path: "e2e/Bar/Bar.sqlproj", Databases: []string{"BarE2E"}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			section, cfg := validSQLServerConfig()
			section.Projects = test.projects
			err := validateSQLServerSection(section, cfg)
			if err == nil || !strings.Contains(err.Error(), "project and database names must be unique across projects") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidateSQLServerSectionAllowsOneProjectToNameMultipleDatabases(t *testing.T) {
	section, cfg := validSQLServerConfig()
	section.Projects[0].Databases = []string{"AccountsDev", "AccountsE2E"}
	if err := validateSQLServerSection(section, cfg); err != nil {
		t.Fatalf("validateSQLServerSection: %v", err)
	}
	want := []string{"AccountsDev", "AccountsE2E"}
	for i, database := range want {
		if section.Projects[0].Databases[i] != database {
			t.Fatalf("databases = %v", section.Projects[0].Databases)
		}
	}
}
