package devdb

// The team-shared SQL-project whitelist. Which projects the DB workflow
// publishes is a team decision, not a per-machine or per-env one — a
// developer can't know what SQL-project folders every teammate happens
// to have checked out, so an explicit allowlist (not a scan-minus-blocklist)
// is the only predictable range. It lives in the shared envs/data/
// db-projects.yaml sibling file (the same convention as claim.yaml), so
// one list covers every environment and ships with the env repo.

import (
	"path/filepath"
	"strings"

	"github.com/iml885203/orbit/config"
	"gopkg.in/yaml.v3"
)

func init() {
	config.RegisterExtensionSection("db_projects", config.ExtensionSection{
		Decode:  decodeDBProjectsSection,
		Default: sharedDBProjectsDefault,
	})
}

// DBProjectsConfig is the allowlist of SQL-project directory names the DB
// workflow publishes. Matching is case-insensitive (see dbProjectAllowlist):
// the same project is cased differently across checkouts (billing.payment
// vs Billing.Payment) and no one list spelling can be right for everyone.
type DBProjectsConfig struct {
	Projects []string `yaml:"projects"`
}

func decodeDBProjectsSection(node *yaml.Node, _ string) (any, error) {
	var c DBProjectsConfig
	if err := config.DecodeStrict(node, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// sharedDBProjectsDefault loads the shared envs/data/db-projects.yaml
// allowlist — the same sibling-file convention as claim.yaml. An absent
// or unreadable file means "no allowlist" (no databases), never a config
// failure, so an env without the file simply has no DB workflow.
func sharedDBProjectsDefault(cfgPath string) (any, error) {
	var c DBProjectsConfig
	found, err := config.LoadSharedSiblingYAML(cfgPath, "db-projects.yaml", &c)
	if !found || err != nil {
		return nil, nil // absent or unreadable/malformed → no allowlist
	}
	return &c, nil
}

// dbProjectAllowlist returns the case-folded set of allowed project
// directory names, or nil when no allowlist is configured. Case-folding
// is what lets one shared list match a folder whatever its casing on a
// given machine.
func dbProjectAllowlist(cfg *config.Config) map[string]bool {
	c, _ := cfg.Extension("db_projects").(*DBProjectsConfig)
	if c == nil || len(c.Projects) == 0 {
		return nil
	}
	set := make(map[string]bool, len(c.Projects))
	for _, name := range c.Projects {
		if n := strings.TrimSpace(name); n != "" {
			set[strings.ToLower(filepath.Base(n))] = true
		}
	}
	return set
}
