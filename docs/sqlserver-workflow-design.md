# SQL Server workflow design for 1.0

Status: accepted product direction; implementation pending.

## Decision

Orbit 1.0 remains a database-neutral local orchestrator. It ships one official,
optional database workflow for SQL Server Database Projects. Orbit does not
claim that dacpac publish, migration runners, document databases, and snapshot
restore share one provider model.

The command remains concise:

```text
orbit db list
orbit db diff <database>
orbit db publish <database>
orbit db reset <database>
```

Help, output, Doctor, and dashboard identify the feature as **SQL Server
Database Projects**.

## Activation

The workflow is enabled only by an explicit top-level section:

```yaml
sqlserver:
  target: sql-server
  username: sa
  password_env: MSSQL_SA_PASSWORD
  projects:
    - path: database/Accounts/Accounts.sqlproj
    - path: database/Orders/Orders.sqlproj
```

There is one target per environment in 1.0.

Orbit does not activate the workflow from:

- a container named `sql-server`;
- an image containing `mssql` or `sqlserver`;
- the presence of an SA password;
- a discovered `.sqlproj`;
- per-machine DB settings.

Detection may suggest configuration in a future assisted authoring command,
but runtime behavior never depends on a guess.

## Configuration semantics

`target` names a container in the same environment. Its published host port is
the SQL Server endpoint.

`projects` is an explicit list of `.sqlproj` paths relative to the workspace
root. Direct paths replace directory-name allowlists, workspace scanning,
`ORBIT_DB_ROOT`, and the separate `data/db-projects.yaml` source of truth.

`username` defaults to `sa` but is configurable.

`password_env` names an environment key on the target container. Orbit reads
the resolved value at runtime and never writes it to settings, status, logs, or
JSON output.

Unknown fields, a missing target, duplicate project paths, paths outside the
workspace, absent credentials, and a target without a published SQL port fail
validation with a field-specific remedy.

## Feature boundary

Without `sqlserver`:

- no SQL Server navigation or dashboard panels appear;
- Doctor performs no .NET, sqlpackage, image, credential, or DB-root checks;
- `orbit init` asks no database questions;
- core configuration does not inspect image names or SA variables;
- `orbit db` explains how a maintainer enables the workflow, without implying
  the current environment is broken.

With `sqlserver`:

- Doctor checks the configured target, credentials, project files, .NET SDK,
  and sqlpackage;
- the dashboard labels the page `SQL Server` and the content
  `Database Projects`;
- command and API errors identify the target and project involved;
- diff and publish operate only on declared projects.

## Safety

- `diff` is read-only.
- `publish` blocks possible data loss by default.
- `publish --force` is explicit and confirmed.
- `reset` states that local data will be discarded and is confirmed.
- Human CLI, JSON metadata, dashboard confirmations, and the bundled agent
  skill use the same destructive classification.
- A failed operation retains a readable log and leaves successful unrelated
  databases identifiable.

## Migration before 1.0

Pre-1.0 users migrate manually:

1. Replace `sql_projects.target` with `sqlserver.target`.
2. Convert the shared project allowlist into explicit `.sqlproj` paths.
3. Move the target username and password-key name into `sqlserver`.
4. Remove `ORBIT_DB_ROOT` or the `db_root` setting after validation.

Orbit does not carry a permanent compatibility layer for the private pre-1.0
schema. Migration documentation and validation errors are preferred over
hidden fallback behavior.

## Deferred until a real second workflow exists

- A database provider interface.
- Multiple SQL Server targets in one environment.
- PostgreSQL, MySQL, or MongoDB schema lifecycle commands.
- A generic publish/migrate/reset vocabulary that erases provider semantics.
