# Configuration reference

[English](./configuration.md) · [繁體中文](./configuration.zh-TW.md)

Full YAML schema for an Orbit environment file. A project may keep
`orbit.yaml` beside its code, or a team may distribute files through
`~/.orbit/envs/` and select one with `orbit switch <name>`. See
`envs/example.yaml` for a runnable example.

**Contents**

- [Config selection](#config-selection)
- [Top-level structure](#top-level-structure)
- [`settings`](#settings)
- [`tracing`](#tracing)
- [`containers`](#containers)
- [`services`](#services)
- [`groups`](#groups)
- [`externals`](#externals)
- [Optional extension: `sqlserver`](#sqlserver)
- [Extension-owned sections](#extension-owned-sections)
- [User settings (`~/.orbit/settings.json`)](#user-settings-orbitsettingsjson)
- [Variable substitution](#variable-substitution)
- [Per-instance overrides](#per-instance-overrides)

## Config selection

Orbit resolves one config for each command in this order:

1. `--config <path>` / `-c <path>`;
2. the nearest `orbit.yaml` in the current directory or one of its parents;
3. the environment selected with `orbit switch`;
4. the distribution's default environment.

This makes commands run inside a project use that project's intent without a
repeated flag. `orbit status --json` reports
`data.environment.source: "project"` and the exact `selected_path` when this
happens. Once a local file is promoted to a shared environment repository,
remove the project copy so the shared file is the single source of truth.

## Top-level structure

```yaml
version: "3"
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
| `version` | string | yes | Schema version. Must be `"3"` — a managed shared env is refreshed with `orbit env sync`, while a project-local file is migrated by its maintainer; when the env is newer than Orbit, run `orbit update` |
| `previewOnly` | bool | no | Env can be inspected on the dashboard but not activated on this machine (guards against misclicks; not a security boundary) |
| `settings` | object | no | Global timeouts and polling intervals |
| `tracing` | object | no | Built-in local OpenTelemetry receiver (on by default — an absent section auto-enables it; add `enabled: false` to opt out) |
| `containers` | map | no | Docker container definitions |
| `services` | map | no | Dev-process service definitions |
| `groups` | map | no | Named service sets — startup batching and dashboard clustering |
| `externals` | map | no | Non-orbit producers/consumers rendered as placeholder graph nodes |
| `sqlserver` | object | no | Explicit opt-in for SQL Server Database Projects |
| `<extension-key>` | any | no | Feature-owned configuration registered by an extension compiled into this binary; accepted keys and shapes depend on the distribution |

Orbit decodes the core schema and registered extension sections strictly.
Unknown keys and misspelled fields fail before any container or host process
starts, and the error identifies the offending field and source line. Orbit
does not silently ignore configuration it cannot understand.

### Migrating schema 2 to 3

Schema 3 replaces database-specific seed fields with one command that runs
inside the container. No automatic migration edits project files.

```yaml
# schema 2
version: "2"
seed:
  type: mongo
  database: app
  files: [./seed.js]

# schema 3
version: "3"
seed:
  command: mongosh --quiet app
  files: [./seed.js]
```

For a shared environment repository, its maintainer commits the schema-3 file
and users run `orbit env sync`. For a project-local `orbit.yaml`, update the
file directly using the mapping above. Credentials referenced by the command
belong in the container's `environment`, not in seed-specific fields.

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
| `health_check_interval` | duration | `5s` | How often to run `http` / `tcp` / `exec` / Docker `healthcheck` probes |
| `docker_poll_interval` | duration | `2s` | How often the container poller calls `docker inspect` |
| `health_check.timeout` | duration | `5s` | Per-probe timeout applied when a `health_check` omits `timeout` |
| `health_check.retries` | int | `12` | Startup retry count applied when a `health_check` omits `retries` (≈1 minute at the default 5s interval). After the budget is spent, Orbit keeps probing every 10s and returns the resource to healthy when it recovers |
| `health_check.failure_threshold` | int | `3` | Consecutive runtime probe failures required before a healthy resource becomes degraded. One successful probe recovers it. `log` checks are readiness-only and are not continuously monitored |

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

Tracing is **on by default**. Omitting the section or adding it only to tune
`otlp_port` / `max_traces` keeps tracing enabled. Only an explicit
`enabled: false` opts out.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Run the receiver and inject `OTEL_*` env into dev services. Only explicit `false` turns it off |
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
| `ports` | map | no | `alias: "hostPort:containerPort"`. A single endpoint also supplies an omitted `health_check.port` |
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

### Native database clients

`orbit query redis`, `orbit query mongo`, and `orbit query postgres` run the
client already present inside a configured container. A `redis`, `mongo`,
`postgres`, or `postgresql` port alias makes the target discoverable even when
the container has a domain-specific name:

```yaml
containers:
  primary-data:
    image: postgres:18
    ports:
      postgres: "5432:5432"
```

When exactly one container matches, Orbit selects it automatically. Multiple
matches are never resolved by map order: the command lists their names and
requires `--container <name>`. For another database client, use
`orbit exec <container> <client...>`.

These commands are connection conveniences, not a generic schema lifecycle.
The optional `sqlserver` workflow owns SQL Server Database Project diff,
publish, and reset semantics because those operations do not map honestly onto
every database.

### Ports that may move

Project ports are fixed by default: a conflict remains an error because silently
moving an API or database can break tools outside Orbit. Portable demos and
similar disposable environments can opt an individual host port into automatic
conflict recovery with an `ORBIT_AUTO_PORT_*` fallback:

```yaml
containers:
  redis:
    image: redis:7.4-alpine
    ports:
      redis: "${ORBIT_AUTO_PORT_DEMO_REDIS:-26379}:6379"
    health_check:
      type: tcp
      port: ${ORBIT_AUTO_PORT_DEMO_REDIS:-26379}

services:
  demo-api:
    type: python
    path: .
    command: python3 app.py
    url: http://localhost:${ORBIT_AUTO_PORT_DEMO_API:-28080}
    ports:
      http: "${ORBIT_AUTO_PORT_DEMO_API:-28080}"
    health_check:
      type: http
      path: /health
      port: ${ORBIT_AUTO_PORT_DEMO_API:-28080}
```

Orbit keeps the preferred value when it is available. Otherwise it selects an
available host port, updates the matching health check and loopback URL, and
injects `<ALIAS>_PORT` into the host service. A service with one port also
receives the conventional `PORT` variable. `orbit status` and
`orbit open <service>` always use the selected runtime port. A managed
container keeps the same selected port across daemon restarts.

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
  port: <int>           # http, tcp — optional for one port; http prefers the "http" alias
  path: /health         # http
  command: [string]     # exec
  pattern: "ready"      # log — regex against container stdout
  interval: 5s          # poll cadence (defaults to settings.health_check_interval)
  timeout: 30s
  retries: 10
  failure_threshold: 3 # consecutive runtime failures before degraded
```

| Type | Semantics |
|---|---|
| `http` | `GET http://localhost:<port><path>`, accept any 2xx |
| `tcp` | Open a TCP connection to the port |
| `exec` | Run `command` inside the container, treat exit 0 as healthy |
| `log` | Tail container logs for a regex match (one-shot readiness signal; not a runtime liveness probe) |
| `healthcheck` | Use the image's own `HEALTHCHECK` as reported by `docker inspect` |

For `http` and `tcp`, omit `port` when the resource declares one port. An
`http` check also selects the `http` alias when other ports exist. Orbit asks
for an explicit health-check port only when the endpoint remains ambiguous.

### `seed`

```yaml
seed:
  command: mongosh --quiet app
  files:
    - envs/seeds/mongo/001-something.js
```

Running `orbit seed` executes `command` inside the running container, providing
each file on standard input in order. The command is database-neutral and can
invoke any client shipped by the image. For example, PostgreSQL can use:

```yaml
seed:
  command: psql -v ON_ERROR_STOP=1 -U app -d app
  files:
    - envs/seeds/postgres/001-schema.sql
    - envs/seeds/postgres/002-data.sql
```

Container environment variables are available to the command, so credentials
remain in the container environment instead of Orbit seed fields. Applied
seeds are recorded by command and file content; re-running `orbit seed` skips
them unless you pass `--force` (a CLI flag, not a config field). Changing the
command or file causes Orbit to ask for `--force` rather than silently applying
it to a different target.

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
    type: python                   # dotnet is special; otherwise use a runtime label
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
| `command` | string | no | The process to run for non-dotnet types. Quotes group arguments and `$VAR` expands from the service environment; Orbit executes the result directly without an implicit shell |
| `watch` | bool | no | dotnet only: run `dotnet watch` instead of compile-and-run (default `false`) |
| `url` | string | no | Canonical URL; `orbit open <service>` uses this. Omit it when an `http` or `https` port already identifies the endpoint |
| `ports` | map | no | Ports the service listens on. A single port supplies an omitted health-check port; `http`/`https` also supplies the default open URL |
| `env` | map | no | Process env. `${VAR}` substituted at load time |
| `build_env` | map | no | dotnet only: env passed to `dotnet build`, not to the running process |
| `env_toggles` | map | no | Dashboard-controlled on/off flipping of individual env keys |
| `pre_start` | list | no | Shell commands run sequentially before the service starts; output is streamed to the service log and a non-zero exit aborts startup |
| `health_check` | object | no | Same shape as container `health_check` |
| `depends_on` | list | no | Names that must be `Healthy` before this starts |
| `kafka` | object | no | `produces` / `consumes` topic lists — drawn as async edges on the graph |

For example, `command: python3 -m http.server "$PORT"` receives Orbit's
selected single-service port as one argument. Use an explicit shell command
such as `sh -c "..."` only when the process genuinely needs pipes, redirects,
or other shell operators.

### Service dependency URLs

When a service in `depends_on` declares `url` or an `http`/`https` port, Orbit
injects that endpoint into the dependent process as `<DEPENDENCY_NAME>_URL`:

```yaml
services:
  catalog-api:
    type: python
    path: ./catalog
    command: python3 main.py
    url: http://localhost:${ORBIT_AUTO_PORT_CATALOG:-3001}
    ports:
      http: "${ORBIT_AUTO_PORT_CATALOG:-3001}"

  checkout-api:
    type: python
    path: ./checkout
    command: python3 main.py
    depends_on: [catalog-api]
```

`checkout-api` receives `CATALOG_API_URL`. If Orbit moves the upstream port,
the injected URL contains the selected runtime port. The environment declares
the endpoint once; downstream services do not duplicate it. An explicit
`env.CATALOG_API_URL` remains an intentional override and wins over injection.
The dashboard attributes injected values to their dependency.

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

Group names are validated before startup. An unknown name fails with the
available groups instead of becoming a successful no-op. Service names,
`--group`, and `--infra` are separate `up` selection modes and cannot be
combined.

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

This complete example includes the target container, its persistent storage,
and the workflow section:

```yaml
containers:
  database:
    image: mcr.microsoft.com/mssql/server:2022-latest
    ports:
      mssql: "14333:1433"
    environment:
      ACCEPT_EULA: "Y"
      MSSQL_SA_PASSWORD: "${SQLSERVER_PASSWORD}"
    volumes:
      - orbit-sqlserver:/var/opt/mssql
    health_check:
      type: tcp
      port: 14333
      retries: 30

sqlserver:
  target: database
  username: sa
  password_env: MSSQL_SA_PASSWORD
  projects:
    - path: database/Accounts/Accounts.sqlproj
    - path: database/Orders/Orders.sqlproj
```

Set `SQLSERVER_PASSWORD` in the host environment before starting Orbit. The
Microsoft image requires `MSSQL_SA_PASSWORD` to initialize itself; Orbit reads
the same resolved key because `password_env` names it. If an image requires a
different bootstrap key, declare both keys on the target container and point
`password_env` at the one Orbit should read.

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

`workspace_root` is a first-class setting. `orbit init` asks for it only when
the selected environment actually references workspace projects; it never
stores an arbitrary current directory for a containers-only or self-contained
environment. You can also set it before the daemon starts with
`orbit settings set workspace-root <path>`. It is exported to env configs as
`WORKSPACE_ROOT`. Persist any other referenced variable without editing JSON:

```bash
orbit settings set-env API_ROOT /path/to/api
```

These values are stored under `user_env`.

After selecting an environment, `orbit doctor` validates resolved service
paths. For a `type: python` service launched by a Python interpreter, a
project-root `requirements.txt` is also checked against that exact interpreter
without downloading or installing anything. If requirements are unsatisfied,
Doctor gives an explicit `pip install` command. It uses the active virtual
environment when the command names one; otherwise it keeps packages in the
user installation and adds pip's externally-managed override only when that
interpreter reports it is required.

For a `type: node` service whose `package.json` declares dependencies, Doctor
also verifies that packages are installed. It chooses the install command from
`packageManager`, then the lockfile, and otherwise uses npm. Workspace metadata
and installed packages may live at the nearest repository root. This applies
to direct commands such as `node server.js` as well as package-manager commands,
and Doctor checks that the selected package manager itself is installed. A
missing manager is the only package-related next step; Orbit checks project
packages after that tool becomes available.

`orbit up` performs the same checks before starting containers or host
processes, so an invalid workspace or known-missing project dependency cannot
leave a partially started environment. Dependency installation remains an
explicit user action.

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
