// Shared preamble of the `orbit sqlserver` subcommand family: dialing the daemon
// and gating on the DB workflow (see internal/daemon/db_workflow.go for the
// daemon side of the gate).

package devdb

import (
	"fmt"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/sqlpublish"
)

// requireDBWorkflow rejects db subcommands early when the daemon reports the
// active env doesn't opt into the DB workflow, so adopters get one clear message
// instead of a SQL-image-specific failure downstream. Fail-open semantics
// (an older daemon without the field counts as configured) live on the
// daemon type; this helper only maps them to the CLI error taxonomy.
func requireDBWorkflow(client *daemon.Client) error {
	meta, err := fetchDevDBMeta(client)
	if err != nil {
		return fmt.Errorf("checking db workflow availability: %w", err)
	}
	if !meta.WorkflowConfigured() {
		return sqlServerNotConfiguredError{configPath: meta.EnvironmentPath}
	}
	return nil
}

const sqlServerConfigurationGuide = "https://github.com/iml885203/orbit/blob/main/docs/configuration.md#sqlserver"

type sqlServerNotConfiguredError struct {
	configPath string
}

func (e sqlServerNotConfiguredError) Error() string {
	location := "the active environment"
	if e.configPath != "" {
		location = fmt.Sprintf("%q", e.configPath)
	}
	return fmt.Sprintf("SQL Server workflow is not enabled for %s — choose an environment that enables it or add sqlserver.target and sqlserver.projects to its source config; see %s", location, sqlServerConfigurationGuide)
}

func (e sqlServerNotConfiguredError) Unwrap() error {
	return cli.ErrNotConfigured
}

func (e sqlServerNotConfiguredError) CLIJSONHint() string {
	return "Choose an environment that enables SQL Server, or edit its source config using the linked schema guide."
}

// dialDBWorkflow is the shared preamble of every daemon-backed db
// subcommand: dial the daemon, then gate on the DB workflow. One entry
// point so a future db subcommand can't forget the gate.
func dialDBWorkflow() (*daemon.Client, error) {
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return nil, err
	}
	if err := requireDBWorkflow(client); err != nil {
		return nil, err
	}
	return client, nil
}

// publishConnOptsFromClient assembles the host connection opts the
// publish and snapshot operations need. Port AND credentials both
// resolve from the daemon's publish target (the CLI has no config of
// its own here) — mixing the target's port with another container's
// password would break any env whose target isn't sql-server.
func publishConnOptsFromClient(client *daemon.Client, dbName string) (sqlpublish.Opts, error) {
	meta, err := fetchDevDBMeta(client)
	if err != nil {
		return sqlpublish.Opts{}, err
	}
	if meta.SQLServerPort <= 0 {
		return sqlpublish.Opts{}, fmt.Errorf("publish target host port unavailable from the active env")
	}
	target := meta.SQLServerTarget
	if target == "" {
		return sqlpublish.Opts{}, fmt.Errorf("SQL Server target unavailable from the active environment")
	}
	serviceName := strings.TrimPrefix(target, "orbit-")
	if !containerRunning(target) {
		return sqlpublish.Opts{}, fmt.Errorf("SQL Server target %s is stopped — start it with `orbit up %s`", serviceName, serviceName)
	}
	status, err := client.Status()
	if err != nil {
		return sqlpublish.Opts{}, fmt.Errorf("checking SQL Server readiness: %w", err)
	}
	state := ""
	for _, service := range status.Resources {
		if service.Name == serviceName {
			state = service.State
			break
		}
	}
	if state != "healthy" {
		if state == "" {
			state = "not available"
		}
		return sqlpublish.Opts{}, fmt.Errorf("SQL Server target %s is %s — start it with `orbit up %s`", serviceName, state, serviceName)
	}
	if meta.SQLServerUsername == "" || meta.SQLServerPasswordEnv == "" {
		return sqlpublish.Opts{}, fmt.Errorf("SQL Server credentials unavailable from the active environment")
	}
	password, err := resolveContainerPassword(target, meta.SQLServerPasswordEnv)
	if err != nil {
		return sqlpublish.Opts{}, err
	}
	targetID := publishTargetIdentity(meta.EnvironmentPath, target, meta.SQLServerImage)
	return sqlpublish.Opts{DB: dbName, Host: "localhost", Port: meta.SQLServerPort, TargetID: targetID, User: meta.SQLServerUsername, Password: password}, nil
}
