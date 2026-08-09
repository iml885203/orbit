# Configuration reference

[English](./configuration.md) · [繁體中文](./configuration.zh-TW.md)

Full YAML schema for an Orbit environment file. A project may keep
`orbit.yaml` beside its code, or a team may distribute files through
`~/.orbit/envs/` and select one with `orbit switch <name>`. See
`envs/example.yaml` for a runnable example.

**Contents**

- [Config selection](#config-selection)
- [Top-level structure](#top-level-structure)
- [`extends`](#extends)
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
- [Low-level runtime overrides](#low-level-runtime-overrides)

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
extends:             # optional: inherit everything from another env file
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
| `version` | string | yes | Schema version. Must be `"3"` — a managed shared env is refreshed with `orbit source sync`, while a project-local file is migrated by its maintainer; when the env is newer than Orbit, run `orbit update` |
| `extends` | string | no | Path of an env file this one inherits from, resolved relative to this file's directory. One level only — see [`extends`](#extends) |
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
starts. The error identifies the offending field and source line and suggests
the closest supported field or enum value when the correction is unambiguous.
The same guidance appears through `orbit doctor`, `orbit up`, and
`orbit inspect --json`; Orbit does not silently ignore configuration it cannot
understand.

The former top-level `previewOnly` field has been removed. Delete it from
existing environment files; Orbit rejects the obsolete field with migration
guidance. Every valid environment can now be selected, activated, inspected,
and managed.

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
and users run `orbit source sync`. For a project-local `orbit.yaml`, update the
file directly using the mapping above. Credentials referenced by the command
belong in the container's `environment`, not in seed-specific fields.

## `extends`

An env file can inherit from another and state only what differs:

```yaml
# e2e.yaml
extends: backoffice.yaml

services:
  api:
    env:
      ASPNETCORE_ENVIRONMENT: Docker
```

The merge rules are deliberately small:

- Mappings merge key by key, recursively: a key the child names is
  overridden (or, for a nested mapping, refined); a key the child does not
  name is inherited.
- Every non-mapping value — scalar, list, or explicit null — is replaced
  wholesale. There is no list-append rule and no delete marker.
- One level only: the extended file must not itself contain `extends`.

The path must be relative; it resolves against the extending file's
directory, so a synced source's `envs/e2e.yaml` can extend its sibling
`envs/backoffice.yaml` or `envs/base/backoffice.yaml`. Placement matters
twice: files in subdirectories don't appear as selectable envs, and
`orbit source sync` validates every top-level `*.yaml` standalone — a
parent that is not a complete valid env on its own (for example one that
leaves `version` to its children) must therefore live in a subdirectory
such as `envs/base/`. `${VAR}` substitution runs on each file independently
before the merge, and the merged result is validated exactly like a single
file, so `version` may be inherited from the parent.

Two operational notes:

- Orbit releases without `extends` support reject the key as unknown, so a
  shared environment repository adopting it requires its consumers to update
  Orbit first.
- The daemon's "env file edited" staleness signal watches the selected file
  only, not its parent. After editing a parent, re-apply the environment
  (`orbit switch <env>`) to pick the change up.

## `settings`

Global knobs applied across the env.

```yaml
settings:
  shutdown_timeout: 30s
  health_check_interval: 5s
  docker_poll_interval: 2s
  image_pull_concurrency: 0
```

| Field | Type | Default | Description |
|---|---|---|---|
| `shutdown_timeout` | duration | `30s` | Max time to wait for graceful stop before SIGKILL |
| `health_check_interval` | duration | `5s` | How often to run `http` / `tcp` / `exec` / Docker `healthcheck` probes |
| `docker_poll_interval` | duration | `2s` | How often the container poller calls `docker inspect` |
| `image_pull_concurrency` | int | `0` | Maximum distinct Docker image pulls at once; `0` keeps unlimited parallel pulls. Concurrent requests for the same image and platform always share one pull |
| `health_check.timeout` | duration | `5s` | Per-probe timeout applied when a `health_check` omits `timeout` |
| `health_check.retries` | int | `12` | Startup retry count applied when a `health_check` omits `retries` (≈1 minute at the default 5s interval). After the budget is spent, Orbit keeps probing every 10s and returns the resource to healthy when it recovers |
| `health_check.failure_threshold` | int | `3` | Consecutive runtime probe failures required before a healthy resource becomes degraded. One successful probe recovers it. `log` checks are readiness-only and are not continuously monitored |

Duration strings accept Go format: `500ms`, `10s`, `2m`, `1h30m`.

The pull limit changes only image download and extraction concurrency. Image
inspection, container creation, health checks, and host-process startup remain
independent. Each container starts as soon as its own image is ready; Orbit does
not wait for every image in the environment. Use `1` for Docker daemons whose
storage driver performs poorly under concurrent extraction.

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
| `user` | string | no | Container user for `docker --user`, e.g. `"0:0"` — needed by images that require root on a fresh named volume |
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

### Port resolution

The default runtime treats each declared host port as fixed. A conflict is an
error that names the owning process and the remedy, because a silently
different address breaks consumers Orbit does not manage — hardcoded env
values, bookmarks, and test configuration. `orbit doctor` reports occupied
declared ports with their owners before anything starts.

A named runtime selected with `--instance` treats declared host ports as
preferences and persists available resolved ports for stable restarts. Consume
the actual endpoints reported by `up`, `status`, or `instance list`. The full
ownership and cleanup model is documented in
[Isolated runtime instances](instances.md).

Orbit injects `<ALIAS>_PORT` into the host service; a service with one port
also receives the conventional `PORT` variable.

The legacy `{preferred, target}` mapping is still parsed and means the same
fixed port as `"host:target"` — the relocation behavior it used to opt into
was removed. Environment substitution works inside port values when a team
wants a machine-specific override without changing the file
(`http: "${API_PORT:-28080}"`).

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

A resource with no explicit `health_check` gets a TCP readiness check when it
declares one port, or an `http` port among several. This applies equally to
host services and containers: declaring the endpoint is enough for Orbit to
wait before releasing dependents. Use an explicit HTTP, log, `exec`, or image
`healthcheck` probe when listening alone is not sufficient proof. A portless
host worker must remain running through a short startup stabilization window
before Orbit releases its dependents; a portless container that needs a
readiness guarantee must declare an explicit probe. When another resource
depends on a container with no probe and Orbit cannot choose one endpoint,
`orbit doctor` warns with the exact `health_check` path to add instead of
silently implying readiness.

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
    kind: backend                 # frontend | backend | infra — graph node tint
    path: ./src/my-api            # defaults to the orbit.yaml directory
    command: pnpm dev
    url: https://localhost:5001
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
| `type` | string | no | Inferred for common Python, Node, Bun, and Go commands. Set `dotnet` explicitly for build-then-run handling (or `dotnet watch` with `watch: true`) |
| `kind` | string | no | `frontend` \| `backend` (default) \| `infra` — colours the graph node; identity only, never health |
| `path` | string | no | Path to `.csproj` (dotnet) or the working directory for `command`; defaults to the directory containing `orbit.yaml` |
| `command` | string | no | The process to run for non-dotnet types. Quotes group arguments and `$VAR` expands from the service environment; Orbit executes the result directly without an implicit shell |
| `watch` | bool | no | dotnet only: run `dotnet watch` instead of compile-and-run (default `false`) |
| `url` | string | no | Canonical URL; `orbit open <service>` uses this. Omit it when an `http` or `https` port already identifies the endpoint |
| `ports` | map | no | Fixed numbers. A single port supplies an omitted health-check port; `http`/`https` also supplies the default open URL |
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
    ports:
      http: 3001

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

`orbit up --group back_office` starts that group's resources plus their
dependencies. `orbit down --group back_office` stops the group's own resources
and leaves shared dependencies available to other groups. A group with
`enabled: false` is skipped unless explicitly requested on the command line.

Group names are validated before a lifecycle action. An unknown name fails
with the available groups instead of becoming a successful no-op. Resource
names, `--group`, and `--infra` are the same separate selection modes for both
`up` and `down`; they cannot be combined.

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

Env configs reference project directories via environment variables (e.g.
`WORKSPACE_ROOT`, `API_ROOT`). If your checkouts live somewhere other than the
default workspace location, set them with `orbit settings`:

```bash
# For WORKSPACE_ROOT, remove the source and add it again with --workspace.
orbit settings set-env API_ROOT /path/to/api
orbit settings list                              # show current values
```

No supported workflow requires editing `settings.json` by hand. The commands
validate the path before storing it, so a typo is reported immediately instead
of surfacing later as a missing service directory.

The source workspace is exported to its env configs as `WORKSPACE_ROOT`.
`orbit init` asks for it only when the selected environment
actually references workspace projects; it never stores an arbitrary current
directory for a containers-only or self-contained environment. It can be set
before the daemon starts. Any other referenced variable is stored under
`user_env` by `set-env`.

For reference, the commands above produce:

```json
{
  "source": { "name": "team", "workspace": "/path/to/workspace" },
  "user_env": {
    "API_ROOT": "/path/to/api"
  }
}
```

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

## Low-level runtime overrides

These variables expose the isolation primitives used by the default runtime.
For parallel checkouts, agents, and CI jobs, prefer `--instance`; see
[Isolated runtime instances](instances.md).

| Variable | Purpose | Default |
|----------|---------|---------|
| `ORBIT_HOME` | Path for `envs/`, socket, PID, settings | `~/.orbit` |
| `ORBIT_NAMESPACE` | Prefix for Docker container / label names | empty (legacy `orbit-<svc>`) |
| `ORBIT_DASHBOARD_PORT` | Pin the dashboard TCP port; without it, conflicts relocate automatically | `19800` |
| `ORBIT_LOG_LEVEL` | Daemon log verbosity (`debug`/`info`/`warn`/`error`) | `info` |

Set low-level overrides before `orbit up` — changing them after the daemon is already running requires `orbit daemon restart`.

## See also

- [docs/architecture.md](architecture.md) — how these fields feed the orchestrator
- [envs/example.yaml](../envs/example.yaml) — complete runnable example
