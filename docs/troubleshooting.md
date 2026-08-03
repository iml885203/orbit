# Troubleshooting

[English](./troubleshooting.md) · [繁體中文](./troubleshooting.zh-TW.md)

Common failure modes, what they mean, and how to fix them. Start with `orbit doctor` — it catches most setup issues and prints the relevant fix.

## Startup

### `orbit.yaml` has an unknown field or value

Run `orbit doctor` from the project. Orbit validates core and extension
sections without starting resources and points to the source line. For a
recognizable typo it also prints the correction, for example
`did you mean "depends_on"?`; apply the suggested edit and run `orbit up`.
If no suggestion appears, compare that section with the
[configuration reference](configuration.md) instead of guessing.

### `orbit up` hangs at "waiting for <service> to be healthy"
The container started but the health check never succeeded.

- **Check logs**: `orbit logs <resource>`. Most startup failures surface there.
- **Heavy first-time initialization**: containers such as a database restoring many schemas can take several minutes on an empty volume. Follow the logs before treating the wait as a hang.
- **Rosetta emulation on Apple Silicon**: any container with `platform: linux/amd64` runs under Rosetta — expect ~2× startup time. Check `docker inspect --format '{{.Platform}}'`.

### `port already in use`
Another process is holding the port Orbit wants.

Orbit reconciles its own namespaced containers and persisted host processes
automatically, including after an abrupt daemon exit. This message therefore
means the current owner could not be matched to the selected Orbit
environment; you do not need to manually clean up normal Orbit resources.

Orbit names the affected resource and port and prints one read-only command to
inspect the current owner. Stop that process through the tool that owns it, or
change the resource's host port in the shared environment, then run
`orbit up` again. Fetching container logs or blindly restarting the resource
cannot release a host port owned by another process.

### Private registry `pull unauthorized` / `pull access denied`
Your Docker client can't pull the image. The neutral `orbit doctor` verifies Docker availability but does not probe private registries. Authenticate with your registry provider, for example:

```bash
docker login <registry-host>
```

If you don't have registry access, ask the image owner or your administrator to grant it.

### `orbit up` errors: "no env configs found in ~/.orbit/envs"
You haven't run `orbit init` (or the sync failed). Fix:

```bash
orbit source add team --url https://git.example.com/your-env-repo.git
orbit switch example
```

### Environment changes are waiting to be applied

Run `orbit env apply`. Orbit validates the updated environment before stopping
anything. Unchanged resources keep their current process or container; changed
resources and their dependents restart. Resources that were already stopped
remain stopped. Daemon-level changes use a full handoff and restore the running
resources afterward.

If you want to download a team update without interrupting the current
environment, use `orbit source sync <name> --no-apply`, then apply it when ready.

## SQL Server

### After `orbit sqlserver publish`, DbGate doesn't show the new object
DbGate caches the schema per connection. Right-click the connection → Disconnect → Connect, or click the refresh icon on the database node. Verify the object is really there:

```bash
orbit sqlserver query "SELECT name FROM YourDB.sys.procedures ORDER BY create_date DESC"
```

### `publish` fails: `CommonFiles.dacpac could not be resolved`
`dotnet build` didn't produce the shared dacpac alongside the DB's own. Clean
the affected project and rebuild:

```bash
dotnet clean /path/to/Database.sqlproj
orbit sqlserver publish <dbname>
```

### `publish` refuses: "data loss might occur"
You're narrowing a column, dropping a table, or similar. Either accept the loss:

```bash
orbit sqlserver publish <dbname> --allow-data-loss     # shows scope and asks for confirmation
```

or refactor the change in smaller steps. For an already reviewed
non-interactive run, use `--allow-data-loss --yes`. To discard local data and apply the
latest schema, run `orbit sqlserver reset <dbname>`.

### My SP disappeared after restarting the configured SQL Server target
Should not happen after the volume-seeding fix. If it does:

1. Confirm the DB's files live in the volume:
   ```bash
   orbit sqlserver query "SELECT physical_name FROM sys.master_files WHERE database_id = DB_ID('YourDB')"
   ```
   Paths should start with `/var/opt/mssql/data/` (the persistent volume). If a database is missing entirely, republish it with `orbit sqlserver publish <db>` (or use `orbit sqlserver publish --all` for every configured database).
2. Check that the volume or bind mount declared by your environment still
   exists and was not recreated recently.

### Removing a SQL Server volume fails: "volume is in use"
The configured SQL Server target is still attached. If you intend to
permanently discard every local database, stop the environment before removing
the volume declared by its config:

```bash
orbit down
docker volume rm <volume-name>
orbit up <sqlserver.target>
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
Orbit normally chooses another available port automatically, and `orbit open`
uses the active address. If you explicitly pinned `ORBIT_DASHBOARD_PORT`,
either unset it or choose another port:

```bash
unset ORBIT_DASHBOARD_PORT
orbit daemon restart
```

### After upgrading, Orbit says an update is ready
This normally means the binary was replaced manually; `orbit update` reconnects
a running environment automatically. Resource mutations pause so they cannot
cross versions. Apply the manual replacement with the exact command printed by
`orbit status`, for example:

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

### `orbit sqlserver publish` reports unavailable SQL Server credentials

Check that `sqlserver.password_env` names a non-empty environment key on the
configured target container, then restart that target with `orbit restart
<target>`. Orbit does not accept a second password override because that could
silently connect to a different target than the active environment declares.

## Diagnosis

When stuck:

1. **`orbit doctor`** — checks Docker, Orbit configuration, daemon state, ports, and required host tools. Reports each with a `CheckPass` / `CheckWarn` / `CheckFail` and a hint. Extensions may contribute additional checks, including distribution-specific registry or repository checks.
2. **`orbit status`** — confirms which services Orbit believes are healthy and where they disagree with reality.
3. **`orbit logs <resource> -f`** — live-tail the actual output.
4. **`ORBIT_LOG_LEVEL=debug orbit daemon restart`** — verbose daemon logs in `~/.orbit/daemon.log`.
5. **`docker ps -a`** / **`docker logs <container>`** — bypass orbit entirely to rule out its bookkeeping.

If you find a failure mode that isn't covered here, add it and open an MR — the catalog's value compounds with every new entry.

## See also

- [docs/architecture.md](architecture.md) — event model and state machine background
- [docs/configuration.md](configuration.md) — YAML field reference
- [docs/development.md](development.md) — development setup and workflows
- [docs/sql-workflow.md](sql-workflow.md) — deeper SQL-specific flows
