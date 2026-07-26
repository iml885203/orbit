# SQL schema workflow

[English](./sql-workflow.md) · [繁體中文](./sql-workflow.zh-TW.md)

Orbit exposes four database commands: `list`, `diff`, `publish`, and `reset`.
Publishing runs entirely on the host — build the SQL project with `dotnet
build`, then push the dacpac to the running sql-server with `sqlpackage`.
The implementation used to manage fast resets stays internal.

## Volume and persistence model

When the sql-server container runs, it mounts a named Docker volume at
`/var/opt/mssql`, where SQL Server writes its `.mdf` / `.ldf` files. Because
the data lives in the volume, your accumulated schema and data are preserved
across:

- `orbit restart sql-server`
- Docker daemon restarts
- Host reboots
- `orbit down` followed by `orbit up`

Data is discarded only when the volume itself is removed:

```bash
orbit down
docker volume rm orbit_sql_server
```

Removing the volume permanently deletes every local database in it. Use
`orbit db reset <dbname>` when only one database needs clean data.

A fresh volume starts empty; `orbit db publish --all` creates and publishes
every configured database.

## When to use which command

| Situation | Command | Cost |
|---|---|---|
| Check whether source changed | `orbit db diff <dbname>` | usually under a second |
| You just changed one stored proc / table | `orbit db publish <dbname>` | ~15s, idempotent, no downtime |
| You merged schema from `main` and want it locally | `orbit db publish <dbname>` (or `--all`) | ~15s per DB, data preserved |
| The live DB is full of bad test data | `orbit db reset <dbname>` | seconds, local data discarded |
| Set up a fresh SQL Server | `orbit db publish --all` | creates and publishes every configured DB |

## `orbit db publish`: the fast generic path

`orbit db publish <db>` builds the SQL project **on the host** (`dotnet
build`) and publishes the dacpac straight to the running sql-server's
published port with the host `sqlpackage` — no image rebuild, no
container-side tooling, native arm64 on Apple Silicon. It is idempotent:
an unchanged project converges to a no-op in seconds, and data is always
preserved (destructive changes are blocked unless `--force`).

The database schema converges to the project: adding, changing, or deleting a
stored procedure, table, or other project object produces the corresponding
create, alter, or drop. Drops that could lose data are reported by `db diff`
and blocked by publish until the user explicitly passes `--force`.

Requirements (checked by `orbit doctor`): the .NET SDK and sqlpackage on
the host — `dotnet tool install -g microsoft.sqlpackage`.

Two separate pieces decide what gets published and where:

- **Which container** publishes receive — the env's optional `sql_projects`
  section, whose only field is `target` (the container name). Absent, the
  feature set auto-detects the `sql-server` container.

  ```yaml
  sql_projects:
    target: sql-server
  ```

- **Which projects** to publish — the team-shared allowlist in
  `envs/data/db-projects.yaml` (a sibling of `claim.yaml`, shipped with the
  env repo so one list covers every env). It names the SQL-project
  directories, matched case-insensitively against your workspace folders:

  ```yaml
  # envs/data/db-projects.yaml
  projects:
    - billing.payment
    - billing.wallet
  ```

  Declaring a directory here is how a project joins the DB workflow — there
  is no scan-and-guess fallback. No allowlist means no databases.

### The whole env at once: `--all`

`orbit db publish --all` publishes every database from the project merge
sequentially, stopping at the first failure. Add `--parallel[=N]` to publish
up to N databases concurrently (only safe on an already-provisioned server).
The dashboard's `Publish all` button does the same through the daemon.

Against an empty SQL Server, the same command creates missing databases and
deploys referenced shared objects. Rerun it after fixing a failed project;
successful databases converge to no-ops.

The official image reads `MSSQL_SA_PASSWORD` (its `SA_PASSWORD` alias
is deprecated) — declare both on the container, as the bundled envs do.

### Clean resets: `orbit db reset`

`orbit db reset <db>` disconnects active clients, discards local data, and
applies the latest schema. No setup command is required. Orbit automatically
chooses a fast restore when available and rebuilds from the SQL project when
needed.

## Dashboard visibility

The Local DB page checks source changes when opened or focused. Each database
shows whether it is in sync and provides Check, Publish, and Reset actions.

### Publish from the dashboard

The Local DB page has per-db Publish and Reset buttons plus Publish all.
Publish streams its output in a log panel; Reset always prompts before
discarding local data.

Only one db operation can run at a time across the daemon — the buttons disable
while another op is in flight. When publish detects possible data loss, it is
blocked until the user reviews the warning and explicitly confirms the
force-publish action.

## See also

- [configuration.md](configuration.md) — `sql_server_image`, `sql_server_pull_policy`, and related settings
- [docs/troubleshooting.md](troubleshooting.md) — broader error catalog
