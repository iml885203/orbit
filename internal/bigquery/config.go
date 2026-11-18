// Package bigquery owns opt-in BigQuery development datasets and local recovery evidence.
package bigquery

import (
	"fmt"
	"github.com/iml885203/orbit/internal/config"
	"gopkg.in/yaml.v3"
	"regexp"
)

type BQConfig struct {
	Identity string             `yaml:"identity" json:"identity"`
	Profiles map[string]Profile `yaml:"profiles" json:"profiles"`
}

type Profile struct {
	ID          string            `yaml:"-" json:"id"`
	Mode        string            `yaml:"mode" json:"mode"`
	JobProject  string            `yaml:"job_project" json:"job_project"`
	DataProject string            `yaml:"data_project" json:"data_project"`
	Location    string            `yaml:"location" json:"location"`
	Dataset     string            `yaml:"dataset,omitempty" json:"dataset,omitempty"`
	Tables      map[string]Table  `yaml:"tables,omitempty" json:"tables,omitempty"`
	Mapping     map[string]string `yaml:"app_mapping,omitempty" json:"app_mapping,omitempty"`
}

type Table struct {
	Schema  string `yaml:"schema" json:"schema"`
	Fixture string `yaml:"fixture" json:"fixture"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$`)
var projectID = regexp.MustCompile(`^[a-z][a-z0-9-]{4,61}[a-z0-9]$`)
var datasetID = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var locationID = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

func init() {
	config.RegisterExtensionSection("bigquery", config.ExtensionSection{
		Decode: func(node *yaml.Node, _ string) (any, error) {
			var c BQConfig
			if err := config.DecodeStrict(node, &c); err != nil {
				return nil, err
			}
			if err := c.validate(); err != nil {
				return nil, err
			}
			return &c, nil
		},
		ValidateFragment: func(node *yaml.Node) error { var c BQConfig; return config.DecodeStrict(node, &c) },
	})
}

func From(cfg *config.Config) *BQConfig {
	if cfg == nil {
		return nil
	}
	c, _ := cfg.Extension("bigquery").(*BQConfig)
	return c
}

func (c *BQConfig) validate() error {
	if !identifier.MatchString(c.Identity) || len(c.Profiles) == 0 {
		return fmt.Errorf("bigquery requires a stable identity and at least one profile")
	}
	for name, p := range c.Profiles {
		if !identifier.MatchString(name) || !projectID.MatchString(p.JobProject) || !projectID.MatchString(p.DataProject) || !locationID.MatchString(p.Location) {
			return fmt.Errorf("profile %q requires valid name, job_project, data_project and location", name)
		}
		p.ID = name
		switch p.Mode {
		case "shared":
			if (len(p.Dataset) > 1024 || !datasetID.MatchString(p.Dataset)) || len(p.Tables) > 0 {
				return fmt.Errorf("shared profile %q requires dataset and cannot configure tables", name)
			}
		case "sandbox":
			if p.Dataset != "" || len(p.Tables) == 0 {
				return fmt.Errorf("sandbox profile %q requires tables and cannot select an existing dataset", name)
			}
			for id, t := range p.Tables {
				if (len(id) > 1024 || !datasetID.MatchString(id)) || t.Schema == "" || t.Fixture == "" {
					return fmt.Errorf("profile %q table %q requires schema and fixture paths", name, id)
				}
			}
		default:
			return fmt.Errorf("profile %q mode must be shared or sandbox", name)
		}
		names := map[string]bool{}
		for field, name := range p.Mapping {
			if (field != "job_project" && field != "dataset" && field != "location") || !envName.MatchString(name) || names[name] {
				return fmt.Errorf("profile %q has invalid or duplicate app_mapping", p.ID)
			}
			names[name] = true
		}
		c.Profiles[name] = p
	}
	return nil
}
