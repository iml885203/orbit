package devdb

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/iml885203/orbit/config"
	"gopkg.in/yaml.v3"
)

const sqlServerSection = "sqlserver"

var environmentKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// SQLServerConfig is the explicit opt-in for SQL Server Database Projects.
// Project paths are relative to the workspace so one shared env works across
// developer machines without a second allowlist or DB-root setting.
type SQLServerConfig struct {
	Target      string                   `yaml:"target"`
	Username    string                   `yaml:"username"`
	PasswordEnv string                   `yaml:"password_env"`
	Projects    []SQLServerProjectConfig `yaml:"projects"`
}

type SQLServerProjectConfig struct {
	Path string `yaml:"path"`
}

func init() {
	config.RegisterExtensionSection(sqlServerSection, config.ExtensionSection{
		Decode:   decodeSQLServerSection,
		Validate: validateSQLServerSection,
	})
}

func decodeSQLServerSection(node *yaml.Node, _ string) (any, error) {
	var section SQLServerConfig
	if err := config.DecodeStrict(node, &section); err != nil {
		return nil, err
	}
	if section.Username == "" {
		section.Username = "sa"
	}
	return &section, nil
}

func validateSQLServerSection(value any, cfg *config.Config) error {
	section, ok := value.(*SQLServerConfig)
	if !ok || section == nil {
		return fmt.Errorf("invalid decoded configuration")
	}
	if section.Target == "" {
		return fmt.Errorf("target is required")
	}
	target, ok := cfg.Containers[section.Target]
	if !ok || target == nil {
		return fmt.Errorf("target %q is not a declared container", section.Target)
	}
	if len(section.Projects) == 0 {
		return fmt.Errorf("projects must declare at least one .sqlproj path")
	}
	seen := map[string]bool{}
	for i, project := range section.Projects {
		clean := filepath.Clean(strings.TrimSpace(project.Path))
		if clean == "." || clean == "" {
			return fmt.Errorf("projects[%d].path is required", i)
		}
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("projects[%d].path %q must stay relative to the workspace", i, project.Path)
		}
		if !strings.EqualFold(filepath.Ext(clean), ".sqlproj") {
			return fmt.Errorf("projects[%d].path %q must point to a .sqlproj file", i, project.Path)
		}
		if seen[clean] {
			return fmt.Errorf("projects[%d].path %q is duplicated", i, project.Path)
		}
		seen[clean] = true
		section.Projects[i].Path = clean
	}
	if section.Username == "" {
		return fmt.Errorf("username is required")
	}
	if section.PasswordEnv == "" {
		return fmt.Errorf("password_env is required")
	}
	if !environmentKeyPattern.MatchString(section.PasswordEnv) {
		return fmt.Errorf("password_env %q is not a valid environment key", section.PasswordEnv)
	}
	password, ok := target.Environment[section.PasswordEnv]
	if !ok || strings.TrimSpace(password) == "" {
		return fmt.Errorf(
			"target %q must declare the %s environment value",
			section.Target, section.PasswordEnv,
		)
	}
	if _, err := publishTargetHostPort(target); err != nil {
		return fmt.Errorf("target %q: %w", section.Target, err)
	}
	return nil
}

func SQLServerFrom(cfg *config.Config) *SQLServerConfig {
	if cfg == nil {
		return nil
	}
	section, _ := cfg.Extension(sqlServerSection).(*SQLServerConfig)
	return section
}
