# Configuration reference

[English](./configuration.md) · [繁體中文](./configuration.zh-TW.md)

Full YAML schema for an orbit env file. Files live in `~/.orbit/envs/` and are selected with `orbit switch <name>`. See `envs/example.yaml` for a runnable example.

**Contents**

- [Top-level structure](#top-level-structure)
- [`settings`](#settings)
- [`tracing`](#tracing)
- [`containers`](#containers)
- [`services`](#services)
- [`groups`](#groups)
- [`externals`](#externals)
- [`sqlserver`](#sqlserver)
- [Extension-owned sections](#extension-owned-sections)
- [User settings (`~/.orbit/settings.json`)](#user-settings-orbitsettingsjson)
- [Variable substitution](#variable-substitution)
- [Per-instance overrides](#per-instance-overrides)

## Top-level structure

```yaml
version: "2"
previewOnly: false   # optional: env is inspectable but not activatable here
settings:            # global timing, poll intervals
tracing:             # optional built-in OpenTelemetry receiver
containers:          # Docker-managed infrastructure
services:            # dev processes (dotnet, node, shell)
groups:              # named service sets for batch start + dashboard clustering
externals:           # placeholder nodes for non-orbit systems (kafka edges)
<extension-key>:     # optional section owned by a compiled-in extension
```

| Key | Type | Required | Purpose |
|---|---|---|---|
| `version` | string | yes | Schema version. Must be `"2"` — a mismatch fails the load with a hint to run `orbit env sync` (env too old) or upgrade orbit (env too new) |
| `previewOnly` | bool | no | Env can be inspected on the dashboard but not activated on this machine (guards against misclicks; not a security boundary) |
| `settings` | object | no | Global timeouts and polling intervals |
| `tracing` | object | no | Built-in local OpenTelemetry receiver (on by default — an absent section auto-enables it; add `enabled: false` to opt out) |
| `containers` | map | no | Docker container definitions |
| `services` | map | no | Dev-process service definitions |
| `groups` | map | no | Named service sets — startup batching and dashboard clustering |
| `externals` | map | no | Non-orbit producers/consumers rendered as placeholder graph nodes |
| `sqlserver` | object | no | Explicit opt-in for SQL Server Database Projects |
| `<extension-key>` | any | no | Feature-owned configuration registered by an extension compiled into this binary; accepted keys and shapes depend on the distribution |

## `settings`

Global knobs applied across the env.

```yaml
settings:
  shutdown_timeout: 30s
  health_check_interval: 5s
  docker_poll_interval: 2s
```

| Field | Type | Default | Description |
|---|---|---|---|
| `shutdown_timeout` | duration | `30s` | Max time to wait for graceful stop before SIGKILL |
| `health_check_interval` | duration | `5s` | How often to run `http` / `tcp` / `exec` probes |
| `docker_poll_interval` | duration | `2s` | How often the container poller calls `docker inspect` |
| `health_check.timeout` | duration | `5s` | Per-probe timeout applied when a `health_check` omits `timeout` |
| `health_check.retries` | int | `12` | Default retry count applied when a `health_check` omits `retries` (≈1 minute at the default 5s interval). When the budget is spent, orbit keeps probing `http`/`tcp` checks every 10s and flips the service back to healthy on recovery (other check types stay degraded until a restart) |

Duration strings accept Go format: `500ms`, `10s`, `2m`, `1h30m`.

## `tracing`

Enables Orbit's built-in local OpenTelemetry receiver. When on, the daemon runs
an OTLP/HTTP receiver and injects `OTEL_*` env vars into every dev service so
their spans flow into Orbit. Services must have an OTLP exporter configured to
read these standard environment variables.

```yaml
tracing:
  enabled: false      # opt out — omit the whole section to keep tracing on
  otlp_port: 4318     # OTLP/HTTP receiver port (loopback only)
  max_traces: 1000    # in-memory ring-buffer capacity, in traces
```

Tracing is **three-state and on by default**: omit the `tracing` section
entirely and it is enabled (zero-config, Aspire-style). A present section
with no `enabled:` key reads as opted **out** — so if you add the section
only for `otlp_port` / `max_traces`, set `enabled: true` explicitly.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | on when section absent | Run the receiver and inject `OTEL_*` env into dev services. Absent section → on; explicit `false` → off; present section without the key → off |
| `otlp_port` | int | `4318` | OTLP/HTTP port the receiver binds on `127.0.0.1`. Auto-advances past a conflict unless pinned |
| `max_traces` | int | `1000` | Ring-buffer size; oldest trace is evicted past this. Not persisted — cleared on `orbit down` |

Injected into each dev service when enabled: `OTEL_SERVICE_NAME` (the Orbit
service name), `OTEL_TRACES_EXPORTER=otlp`,
`OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`,
`OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:<otlp_port>`, and
`OTEL_TRACES_SAMPLER=always_on` (local volume is low; sample everything).

See [tracing.md](tracing.md) for the dashboard + CLI workflow.

## `containers`

A container is a Docker service Orbit starts, health-checks, and optionally seeds or initializes.

```yaml
containers:
  <name>:
    image: string
    icon: string                # optional Iconify slug for infra dashboard logo
    platform: string            # optional, e.g. linux/amd64
    pull_policy: string         # always | if_not_present | never
    ports:
      <alias>: "<host>:<container>"
    environment:
      KEY: value
    volumes:
      - volume-name:/mount/path
    command: [string]
    health_check: {...}
    depends_on: [name, ...]
    seed: {...}
    init: {...}
    sidecars: [{...}]
```

### Container fields

| Field | Type | Required | Description |
|---|---|---|---|
| `image` | string | yes | Full image reference, supports `${VAR}` substitution |
| `icon` | string | no | Iconify icon slug for graph dashboard infra logo, e.g. `devicon:postgresql` |
| `platform` | string | no | Platform override (`linux/amd64` for forced emulation) |
| `pull_policy` | string | no | `always` (default), `if_not_present`, `never` |
| `ports` | map | no | `alias: "hostPort:containerPort"`. The alias is a label for Orbit's UI and dependency wiring; it is not resolved by `health_check.port` |
| `environment` | map | no | Container env vars. `${VAR}` is substituted from the host |
| `volumes` | list | no | Docker volume / bind mount strings |
| `command` | list | no | Override the image's default command |
| `entrypoint` | list | no | Override the image's entrypoint |
| `kind` | string | no | `frontend` \| `backend` \| `infra` (default) — graph node tint |
| `health_check` | object | no | When to consider the container ready (see below) |
| `depends_on` | list | no | Names of other containers/services that must be `Healthy` first |
| `seed` | object | no | Run SQL/init scripts once the container is healthy |
| `init` | object | no | Typed init hooks (Kafka topics, Mongo replica set) |
| `sidecars` | list | no | Per-container sidecar containers (web UIs, agents) |

### Infra logos in the dashboard

Graph dashboard nodes and drawer headers show a logo for infra containers. Set `containers.<name>.icon` to an Iconify icon slug when the env needs to control the logo:

```yaml
containers:
  postgres:
    image: postgres:16
    icon: devicon:postgresql
```

If `icon` is omitted, the graph UI shows a generic gear icon. Orbit does not infer logos from container names or image strings.

### `health_check`

```yaml
health_check:
  type: http | tcp | exec | log | healthcheck
  # plus type-specific fields:
  port: <int>           # http, tcp — literal port number, not a ports alias
  path: /health         # http
  command: [string]     # exec
  pattern: "ready"      # log — regex against container stdout
  interval: 5s          # poll cadence (defaults to settings.health_check_interval)
  timeout: 30s
  retries: 10
```

| Type | Semantics |
|---|---|
| `http` | `GET http://localhost:<port><path>`, accept any 2xx |
| `tcp` | Open a TCP connection to the port |
| `exec` | Run `command` inside the container, treat exit 0 as healthy |
| `log` | Tail container logs for a regex match (one-shot, then healthy) |
| `healthcheck` | Use the image's own `HEALTHCHECK` as reported by `docker inspect` |

### `seed`

```yaml
seed:
  type: mongo
  database: PlatformDB
  files:
    - envs/seeds/mongo/001-something.js
```

Runs each file against the container after it reaches `Healthy`, in order.
Applied seeds are recorded; re-running `orbit seed` skips them unless you pass
`--force` (a CLI flag, not a config field). SQL Server seeds are explicit
about the login and container environment key:

```yaml
seed:
  type: sqlserver
  username: sa
  password_env: MSSQL_SA_PASSWORD
  files:
    - envs/seeds/sqlserver/001-something.sql
```

### `init`

Typed initialization hooks that know how to talk to specific container families.

```yaml
init:
  type: kafka_topics
  topics_file: ./data/kafka-topics.yaml
```

```yaml
init:
  type: mongo_rs
  rs_members: ["localhost:27017"]
```

| Type | Purpose |
|---|---|
| `kafka_topics` | Apply a YAML list of topics against the Kafka broker. Idempotent |
| `mongo_rs` | Initiate a MongoDB replica set with the given members |

### `sidecars`

Containers that start alongside the parent and share its lifecycle.

```yaml
sidecars:
  - name: dbgate
    image: dbgate/dbgate:latest
    pull_policy: if_not_present
    ports:
      ui: "13000:3000"
    environment:
      CONNECTIONS: con1
```

## `services`

A service is a dev process Orbit spawns, watches, and can restart.

```yaml
services:
  my-api:
    type: dotnet | node | shell
    kind: backend               # frontend | backend | infra — graph node tint
    path: ./src/MyApi/MyApi.csproj
    command: pnpm dev           # non-dotnet types: the command to run in `path`
    watch: false                # dotnet only: use `dotnet watch` instead of build+run
    url: https://localhost:5001/swagger
    ports:
      http: 5000
      https: 5001
    env:
      KEY: value
    build_env:
      NuGetAudit: "false"       # dotnet only: env for the build step, not the process
    env_toggles:
      KEY:
        label: "Human label"
        description: "What ON vs OFF means"
        default: true
    pre_start: ["./scripts/migrate.sh"]
    health_check: {...}
    depends_on: [redis]
    kafka:
      produces: [topic-a]
      consumes: [topic-b]
```

| Field | Type | Required | Description |
|---|---|---|---|
| `type` | string | yes | `dotnet` gets build-then-run handling (or `dotnet watch` with `watch: true`); any other value (`node`, `shell`, …) runs `command` inside `path` |
| `kind` | string | no | `frontend` \| `backend` (default) \| `infra` — colours the graph node; identity only, never health |
| `path` | string | yes | Path to `.csproj` (dotnet) or the working directory for `command` |
| `command` | string | no | The process to run for non-dotnet types |
| `watch` | bool | no | dotnet only: run `dotnet watch` instead of compile-and-run (default `false`) |
| `url` | string | no | Canonical URL; `orbit open <service>` uses this |
| `ports` | map | no | Ports the service listens on. The alias labels the port for the UI; `health_check.port` takes a literal int |
| `env` | map | no | Process env. `${VAR}` substituted at load time |
| `build_env` | map | no | dotnet only: env passed to `dotnet build`, not to the running process |
| `env_toggles` | map | no | Dashboard-controlled on/off flipping of individual env keys |
| `pre_start` | list | no | Shell commands run sequentially before the service starts; output is streamed to the service log and a non-zero exit aborts startup |
| `health_check` | object | no | Same shape as container `health_check` |
| `depends_on` | list | no | Names that must be `Healthy` before this starts |
| `kafka` | object | no | `produces` / `consumes` topic lists — drawn as async edges on the graph |

### `env_toggles`

Lets the dashboard flip a single env var on or off without editing the YAML.

```yaml
env_toggles:
  ORBIT_VITE_API_TARGET:
    label: "Override API Target"
    description: "ON: use orbit's value / OFF: let project .env win"
    default: true
```

When ON, Orbit injects the value from `env.ORBIT_VITE_API_TARGET`. When OFF, the variable is removed from the process env so whatever is in the service's own `.env` takes effect.

## `groups`

Named collections of services. Two purposes: startup batching (`orbit up
--group <name>`) and visual clustering on the graph dashboard (each group is
drawn as a labeled box around its services).

```yaml
groups:
  back_office:
    enabled: true
    color: "#d97706"          # optional CSS color; stable hue derived from the name when omitted
    services: [api, web, db]
```

`orbit up --group back_office` starts only that group's services (plus their
dependencies). A group with `enabled: false` is skipped unless explicitly
requested on the command line.

## `externals`

Placeholder nodes for systems orbit doesn't manage (an upstream feed, a
3rd-party provider) so async Kafka edges to/from them stay visible on the
graph. Externals never participate in startup.

```yaml
externals:
  upstream-feed:
    label: "Upstream Feed"
    color: "#8b949e"          # optional
    kafka:
      produces: [sports-odds]
      consumes: []
```

## `sqlserver`

Explicitly enables SQL Server Database Projects for this environment. Without
this section Orbit does not show SQL Server UI, checks, or setup guidance.

```yaml
sqlserver:
  target: database
  username: sa
  password_env: MSSQL_SA_PASSWORD
  projects:
    - path: database/Accounts/Accounts.sqlproj
    - path: database/Orders/Orders.sqlproj
```

`target` names a container in this env. `username` defaults to `sa`.
`password_env` names the target container environment key containing the
password; Orbit reads its resolved value at runtime without storing or
printing it. Every project path is a workspace-relative `.sqlproj` file.
Orbit never scans sibling directories or guesses from container names and
images. See [sql-workflow.md](sql-workflow.md).

## Extension-owned sections

The core schema reserves top-level sections registered by feature packages
compiled into the current binary. Their names and shapes are not part of the
neutral core schema; consult the distribution's feature documentation. For
example, `claim` (tunnel) and `sqlserver` (SQL workflow) are registered by
the gate-scanned feature packages. A binary rejects feature-owned sections for
which it has no registered handler.

## User settings (`~/.orbit/settings.json`)

Env configs reference project directories via environment variables (e.g. `WORKSPACE_ROOT`, `API_ROOT`). If your checkouts live somewhere other than the default workspace location, set them in `~/.orbit/settings.json`:

```json
{
  "workspace_root": "/path/to/workspace",
  "user_env": {
    "API_ROOT": "/path/to/api"
  }
}
```

`workspace_root` is a first-class setting (set during `orbit init`, or via `orbit settings set workspace-root <path>`). It is exported to env configs as `WORKSPACE_ROOT`. Any other variable goes under `user_env`.

Restart the daemon to pick up changes:

```bash
orbit daemon restart
```

## Variable substitution

Orbit performs `${VAR}` and `${VAR:-default}` substitution across all string fields at load time. Source of substitution, in order of precedence:

1. Environment variables at the time `orbit` runs
2. User settings in `~/.orbit/settings.json` (e.g. `WORKSPACE_ROOT`)
3. The `:-default` fallback in the config

Undeclared variables evaluate to empty string — use `:-` to give them a meaningful default:

```yaml
path: ${API_ROOT:-~/dev/api}
```

## Per-instance overrides

Isolate multiple Orbit instances on one machine (e2e tests, sandboxes):

| Variable | Purpose | Default |
|----------|---------|---------|
| `ORBIT_HOME` | Path for `envs/`, socket, PID, settings | `~/.orbit` |
| `ORBIT_NAMESPACE` | Prefix for Docker container / label names | empty (legacy `orbit-<svc>`) |
| `ORBIT_DASHBOARD_PORT` | Override dashboard TCP port | `19800` |
| `ORBIT_LOG_LEVEL` | Daemon log verbosity (`debug`/`info`/`warn`/`error`) | `info` |

Set these before `orbit up` — changing them after the daemon is already running requires `orbit daemon restart`.

## See also

- [docs/architecture.md](architecture.md) — how these fields feed the orchestrator
- [envs/example.yaml](../envs/example.yaml) — complete runnable example
