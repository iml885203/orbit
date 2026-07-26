# Troubleshooting

[English](./troubleshooting.md) · [繁體中文](./troubleshooting.zh-TW.md)

Common failure modes, what they mean, and how to fix them. Start with `orbit doctor` — it catches most setup issues and prints the relevant fix.

## Startup

### `orbit up` hangs at "waiting for <service> to be healthy"
The container started but the health check never succeeded.

- **Check logs**: `orbit logs <service>`. Most startup failures surface there.
- **Heavy first-time initialization**: containers such as a database restoring many schemas can take several minutes on an empty volume. Follow the logs before treating the wait as a hang.
- **Rosetta emulation on Apple Silicon**: any container with `platform: linux/amd64` runs under Rosetta — expect ~2× startup time. Check `docker inspect --format '{{.Platform}}'`.

### `port already in use`
Another process is holding the port Orbit wants.

```bash
# macOS / Linux
lsof -i :<port>
# Windows
netstat -ano | findstr :<port>
```

If it's a stale `orbit` child, `orbit down` usually clears it (process-group kill). Otherwise kill the offending PID manually.

### Private registry `pull unauthorized` / `pull access denied`
Your Docker client can't pull the image. The neutral `orbit doctor` verifies Docker availability but does not probe private registries. Authenticate with your registry provider, for example:

```bash
docker login <registry-host>
```

If you don't have registry access, ask the image owner or your administrator to grant it.

### `orbit up` errors: "no env configs found in ~/.orbit/envs"
You haven't run `orbit init` (or the sync failed). Fix:

```bash
orbit env sync --url https://git.example.com/your-env-repo.git
orbit switch example
```

## SQL Server

### After `orbit db publish`, DbGate doesn't show the new object
DbGate caches the schema per connection. Right-click the connection → Disconnect → Connect, or click the refresh icon on the database node. Verify the object is really there:

```bash
docker exec orbit-sql-server /opt/mssql-tools18/bin/sqlcmd \
  -S localhost -U sa -P "<your-sa-password>" -C \
  -Q "SELECT name FROM YourDB.sys.procedures ORDER BY create_date DESC"
```

### `publish` fails: `CommonFiles.dacpac could not be resolved`
`dotnet build` didn't produce the shared dacpac alongside the DB's own. Clean
the affected project and rebuild:

```bash
dotnet clean /path/to/Database.sqlproj
orbit db publish <dbname>
```

### `publish` refuses: "data loss might occur"
You're narrowing a column, dropping a table, or similar. Either accept the loss:

```bash
orbit db publish <dbname> --force     # passes BlockOnPossibleDataLoss=false
```

or refactor the change in smaller steps. To discard local data and apply the latest schema, run `orbit db reset <dbname>`.

### My SP disappeared after `orbit restart sql-server`
Should not happen after the volume-seeding fix. If it does:

1. Confirm the DB's files live in the volume:
   ```bash
   docker exec orbit-sql-server /opt/mssql-tools18/bin/sqlcmd \
     -S localhost -U sa -P "<your-sa-password>" -C \
     -Q "SELECT physical_name FROM sys.master_files WHERE database_id = DB_ID('YourDB')"
   ```
   Paths should start with `/var/opt/mssql/data/` (the persistent volume). If a database is missing entirely, republish it with `orbit db publish <db>` (or use `orbit db publish --all` for every configured database).
2. Check the volume survived: `docker volume inspect orbit_sql_server` should show it exists and wasn't recreated recently.

### `docker volume rm orbit_sql_server` fails: "volume is in use"
The sql-server container is still attached. Stop it first:

```bash
orbit down
docker volume rm orbit_sql_server
orbit up sql-server
```

## Daemon

### `orbit daemon status` says running but `orbit up` can't reach it
Stale pid/socket files. Orbit auto-detects a dead PID and cleans both files on the next check, so just retry:

```bash
orbit daemon status   # or: orbit up
```

If that still fails, remove the files manually as a fallback:

```bash
rm ~/.orbit/orbit.sock ~/.orbit/orbit.pid
orbit daemon start
```

### Dashboard port :19800 won't open
Another process is using it. Override the port for this instance:

```bash
export ORBIT_DASHBOARD_PORT=19801
orbit daemon restart
```

### After upgrading the binary, `orbit status` complains about version mismatch
The daemon is still running the old binary. Restart it:

```bash
orbit daemon restart
```

## File system & permissions

### `~/.orbit/` is read-only or the wrong user
Happens if you installed Orbit as root and now run it as your user. Fix ownership:

```bash
sudo chown -R $(whoami) ~/.orbit
```

## CLI

### `orbit exec` complains the container name doesn't exist
Orbit's default naming is `orbit-<service>`. If you set `ORBIT_NAMESPACE=foo`, the name becomes `foo-<service>`. Check `docker ps --format '{{.Names}}'`.

### `orbit db publish` errors: `SA_PASSWORD not set`
Orbit reads the password from the `orbit-sql-server` container's env by default. If the container isn't running, or is running a non-orbit SQL Server, set it explicitly:

```bash
export SA_PASSWORD="<your-sa-password>"
orbit db publish AppDB
```

## Diagnosis

When stuck:

1. **`orbit doctor`** — checks Docker, Orbit configuration, daemon state, ports, and required host tools. Reports each with a `CheckPass` / `CheckWarn` / `CheckFail` and a hint. Extensions may contribute additional checks, including distribution-specific registry or repository checks.
2. **`orbit status`** — confirms which services Orbit believes are healthy and where they disagree with reality.
3. **`orbit logs <service> -f`** — live-tail the actual output.
4. **`ORBIT_LOG_LEVEL=debug orbit daemon restart`** — verbose daemon logs in `~/.orbit/daemon.log`.
5. **`docker ps -a`** / **`docker logs <container>`** — bypass orbit entirely to rule out its bookkeeping.

If you find a failure mode that isn't covered here, add it and open an MR — the catalog's value compounds with every new entry.

## See also

- [docs/architecture.md](architecture.md) — event model and state machine background
- [docs/configuration.md](configuration.md) — YAML field reference
- [docs/development.md](development.md) — development setup and workflows
- [docs/sql-workflow.md](sql-workflow.md) — deeper SQL-specific flows
