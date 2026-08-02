# SQL Server Database Projects

[English](./sql-workflow.md) · [繁體中文](./sql-workflow.zh-TW.md)

The command family is named `orbit sqlserver` because this optional workflow
implements SQL Server Database Project semantics; it is not Orbit's generic
database abstraction. Redis, MongoDB, and PostgreSQL client conveniences remain
under `orbit query`.

An environment that explicitly enables `sqlserver` gets five Database Project
commands: `list`, `diff`, `publish`, `reset`, and `query`. Other environments
do not show this workflow.
Publishing runs entirely on the host — build the SQL project with `dotnet
build`, then push the dacpac to the configured SQL Server target with
`sqlpackage`.
The implementation used to manage fast resets stays internal.

## Volume and persistence model

The environment config should mount persistent storage at `/var/opt/mssql`,
where SQL Server writes its `.mdf` / `.ldf` files. With that mount, your
accumulated schema and data are preserved
across:

- `orbit restart <sqlserver.target>`
- Docker daemon restarts
- Host reboots
- `orbit down` followed by `orbit up`

Orbit never removes that storage automatically. Removing its volume or bind
mount data permanently deletes every local database. Use `orbit sqlserver reset
<dbname>` when only one database needs clean data.

A fresh volume starts empty; `orbit sqlserver publish --all` creates and publishes
every configured database.

## When to use which command

| Situation | Command | Cost |
|---|---|---|
| Check whether source changed | `orbit sqlserver diff <dbname>` | usually under a second |
| You just changed one stored proc / table | `orbit sqlserver publish <dbname>` | ~15s, idempotent, no downtime |
| You merged schema from `main` and want it locally | `orbit sqlserver publish <dbname>` (or `--all`) | ~15s per DB, data preserved |
| The live DB is full of bad test data | `orbit sqlserver reset <dbname>` | seconds, local data discarded |
| Set up a fresh SQL Server | `orbit sqlserver publish --all` | creates and publishes every configured DB |

## `orbit sqlserver publish`: the everyday path

`orbit sqlserver publish <db>` builds the SQL project **on the host** (`dotnet
build`) and publishes the dacpac straight to the configured target's published
port with the host `sqlpackage` — no image rebuild, no
container-side tooling, native arm64 on Apple Silicon. It is idempotent:
an unchanged project converges to a no-op in seconds, and data is always
preserved (destructive changes are blocked unless `--allow-data-loss`).

Agents and scripts can use `orbit sqlserver publish <db> --json` for this
ordinary path. Success returns an `orbit.cli.v1` envelope naming every
published database. A forced publish never runs in JSON mode: the error
envelope preserves the selected scope and `--allow-data-loss` as a
`destructive: true` manual action, without `--yes`, so a person still sees the
confirmation prompt.

The database schema converges to the project: adding, changing, or deleting a
stored procedure, table, or other project object produces the corresponding
create, alter, or drop. Drops that could lose data are reported by
`orbit sqlserver diff`
and blocked by publish until the user explicitly passes `--allow-data-loss`. A forced
publish shows every affected database and asks for confirmation; use
`--allow-data-loss --yes` only after reviewing the impact when running non-interactively.

Requirements (checked by `orbit doctor`): the .NET SDK and sqlpackage on
the host — `dotnet tool install -g microsoft.sqlpackage`.

One explicit section decides both what gets published and where:

```yaml
sqlserver:
  target: database
  username: sa
  password_env: MSSQL_SA_PASSWORD
  projects:
    - path: database/Accounts/Accounts.sqlproj
    - path: database/Orders/Orders.sqlproj
```

`target` names the container that receives publishes. Project entries are
workspace-relative `.sqlproj` files. There is no image sniffing, conventional
container name, directory scan, or separate per-machine allowlist.

### The whole env at once: `--all`

`orbit sqlserver publish --all` publishes every database from the project merge
sequentially, stopping at the first failure. Add `--parallel[=N]` to publish
up to N databases concurrently (only safe on an already-provisioned server).
The dashboard's `Publish all` button does the same through the daemon.

Against an empty SQL Server, the same command creates missing databases and
deploys referenced shared objects. Rerun it after fixing a failed project;
successful databases converge to no-ops.

`password_env` names the target container key containing the password. Orbit
reads that resolved value only when a DB operation runs and never exposes it
in status, logs, or JSON output.

### Clean resets: `orbit sqlserver reset`

`orbit sqlserver reset <db>` disconnects active clients, discards local data, and
applies the latest schema. No setup command is required. Orbit automatically
chooses a fast restore when available and rebuilds from the SQL project when
needed.

`orbit sqlserver query` is intentionally CLI-only. The dashboard focuses on project
drift, publish, and reset operations rather than embedding a general SQL
console.

## Dashboard visibility

The SQL Server page checks source changes when opened or focused. Each database
shows whether it is in sync and provides Check, Publish, and Reset actions.

### Publish from the dashboard

The SQL Server page has per-db Publish and Reset buttons plus Publish all.
Publish streams its output in a log panel; Reset always prompts before
discarding local data. When no reset point exists yet, the page explains
beforehand that the first reset recreates the database and saves one for later.

Only one db operation can run at a time across the daemon — the buttons disable
while another op is in flight. When publish detects possible data loss, it is
blocked until the user reviews the warning and explicitly confirms the
force-publish action.

## See also

- [configuration.md](configuration.md#sqlserver) — complete target container and `sqlserver` configuration
- [docs/troubleshooting.md](troubleshooting.md) — broader error catalog
