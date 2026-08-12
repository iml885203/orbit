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
	Path                string   `yaml:"path"`
	Databases           []string `yaml:"databases,omitempty"`
	databasesConfigured bool
}

func (project *SQLServerProjectConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("project must be a mapping")
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		switch key.Value {
		case "path":
		case "databases":
			project.databasesConfigured = true
		default:
			return fmt.Errorf("line %d: unknown field %s", key.Line, key.Value)
		}
	}
	type projectConfig SQLServerProjectConfig
	var decoded projectConfig
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	configured := project.databasesConfigured
	*project = SQLServerProjectConfig(decoded)
	project.databasesConfigured = configured
	return nil
}

func init() {
	config.RegisterExtensionSection(sqlServerSection, config.ExtensionSection{
		Decode:           decodeSQLServerSection,
		ValidateFragment: validateSQLServerFragment,
		Validate:         validateSQLServerSection,
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

func validateSQLServerFragment(node *yaml.Node) error {
	var section SQLServerConfig
	return config.DecodeStrict(node, &section)
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
	seenPaths := map[string]bool{}
	seenDatabaseProjects := map[string]int{}
	seenProjectNames := map[string]int{}
	seenIdentifiers := map[string]int{}
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
		if seenPaths[clean] {
			return fmt.Errorf("projects[%d].path %q is duplicated", i, project.Path)
		}
		seenPaths[clean] = true
		projectName := strings.TrimSuffix(filepath.Base(clean), filepath.Ext(clean))
		projectKey := strings.ToLower(projectName)
		if previous, exists := seenProjectNames[projectKey]; exists {
			return fmt.Errorf(
				"projects[%d].path %q and projects[%d].path %q both use project name %q; .sqlproj basenames must be unique",
				i, project.Path, previous, section.Projects[previous].Path, projectName,
			)
		}
		seenProjectNames[projectKey] = i
		if previous, exists := seenIdentifiers[projectKey]; exists && previous != i {
			return crossProjectSQLNameCollision(i, project.Path, previous, section.Projects[previous].Path, projectName)
		}
		seenIdentifiers[projectKey] = i
		if (project.databasesConfigured || project.Databases != nil) && len(project.Databases) == 0 {
			return fmt.Errorf("projects[%d].databases must contain at least one database name when specified", i)
		}
		normalizedProject := project
		normalizedProject.Path = clean
		databases := databaseNamesForProject(normalizedProject)
		for databaseIndex, rawDatabase := range databases {
			database := strings.TrimSpace(rawDatabase)
			if !safeDBName.MatchString(database) {
				return fmt.Errorf("projects[%d].databases[%d] %q is not a valid database name", i, databaseIndex, rawDatabase)
			}
			databaseKey := strings.ToLower(database)
			if previous, exists := seenDatabaseProjects[databaseKey]; exists {
				return duplicateDatabaseName(i, project.Path, previous, section.Projects[previous].Path, database)
			}
			if previous, exists := seenIdentifiers[databaseKey]; exists && previous != i {
				return crossProjectSQLNameCollision(i, project.Path, previous, section.Projects[previous].Path, database)
			}
			seenDatabaseProjects[databaseKey] = i
			seenIdentifiers[databaseKey] = i
			databases[databaseIndex] = database
		}
		section.Projects[i].Path = clean
		section.Projects[i].Databases = databases
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

// duplicateDatabaseName carries the config shape that replaces the rejected
// one: two project files per database is the instinct being corrected, so
// naming the rule alone would leave the user with nothing to write instead.
func duplicateDatabaseName(current int, currentPath string, previous int, previousPath string, name string) error {
	return fmt.Errorf(
		"database name %q is declared by both projects[%d].path %q and projects[%d].path %q; "+
			"to deploy one schema to several databases, keep one entry and give each target a "+
			"distinct name under its `databases:` list",
		name, previous, previousPath, current, currentPath,
	)
}

// crossProjectSQLNameCollision names the command the constraint protects:
// resolveDBArg searches project and database names in one space, so a name
// meaning both would make `publish <name>` ambiguous.
func crossProjectSQLNameCollision(current int, currentPath string, previous int, previousPath string, name string) error {
	return fmt.Errorf(
		"projects[%d].path %q and projects[%d].path %q both expose name %q; "+
			"`orbit sqlserver publish|reset` accepts either a project or a database name, so one "+
			"name cannot mean both — rename the database, or rename the .sqlproj",
		current, currentPath, previous, previousPath, name,
	)
}

func databaseNamesForProject(project SQLServerProjectConfig) []string {
	if project.Databases != nil {
		return append([]string(nil), project.Databases...)
	}
	name := strings.TrimSuffix(filepath.Base(project.Path), filepath.Ext(project.Path))
	return []string{name}
}

func SQLServerFrom(cfg *config.Config) *SQLServerConfig {
	if cfg == nil {
		return nil
	}
	section, _ := cfg.Extension(sqlServerSection).(*SQLServerConfig)
	return section
}
