# Orbit v0.0.37 — predictable database targets

Orbit clarifies its database boundary and makes native container queries safe
when an environment has more than one database of the same type.

## What changed

- `orbit query redis`, `orbit query mongo`, and `orbit query postgres` still
  select a single matching container automatically.
- Multiple matching containers now produce a stable, sorted candidate list and
  require `--container <name>` instead of selecting whichever Go map entry
  happens to appear first.
- An explicit container that is not configured reports the available names
  before Docker is invoked.
- PostgreSQL continues to use the target container's `POSTGRES_USER` and
  `POSTGRES_DB`; `--database` remains only an override.
- Help and configuration docs explain port-label discovery, explicit target
  selection, and the `orbit exec` escape hatch for other native clients.
- The optional `orbit db` surface now identifies itself as **SQL Server
  Database Projects** in contextual help and documentation. Environments that
  do not enable `sqlserver` still see none of that workflow.

## Why it matters

Orbit remains database-neutral as an orchestrator without pretending that
every schema system shares SQL Server's diff, dacpac publish, and snapshot
reset semantics. Common native queries stay convenient, while target selection
is explicit exactly when guessing could send a command to the wrong data.
