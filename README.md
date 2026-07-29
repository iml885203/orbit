# <img src="ui/public/orbit-logo-badge.svg" width="32" height="32" alt=""> Orbit

Run a mixed local-development stack—host processes and containers—from one
declarative environment.

[Install](docs/development.md) · [Configuration](docs/configuration.md) ·
[Architecture](docs/architecture.md) · [Agent CLI](docs/agent-cli.md) ·
[繁體中文](README.zh-TW.md)

> **Pre-1.0 preview:** Orbit is open for early adopters and contributors while
> the 1.0 UX and compatibility contracts are still being finalized. Expect
> breaking changes before `v1.0.0`.

Orbit starts services in dependency order, checks health, streams logs, keeps
the environment alive behind a resident daemon, and exposes the same controls
through a local dashboard and a stable JSON CLI contract.

## Why Orbit?

Local development rarely fits in a single container file. Application services
often run on the host for fast iteration while databases, queues, and caches run
in containers. Orbit gives that whole environment one control plane:

- **Mixed runtimes:** coordinate host processes and containers in one graph.
- **Shared environments:** sync versioned YAML from any Git repository.
- **Agent-ready automation:** use the `orbit.cli.v1` JSON envelope for reliable
  machine control.
- **Local diagnostics:** health, logs, history, traces, and configuration are
  visible from the CLI and dashboard.

See [Why Orbit](docs/why-orbit.md) for design trade-offs and comparisons.

## Install

The installer below installs the latest published preview release, not an
unreleased build from `main`.

```bash
curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh | bash
export PATH="$HOME/.local/bin:$PATH"
```

## First 5 minutes

Start from any directory. Orbit downloads and selects the public demo
environment; you do not need to clone this repository or create a config file:

```bash
orbit init --yes
orbit up
orbit status
orbit open demo-api
```

`orbit init --yes` uses the public
[Orbit demo environment](https://github.com/iml885203/orbit-demo): a
standard-library Python service on the host with Redis in a container. If a
required tool or port is unavailable, setup stops before startup and prints
the specific remedy.

Refresh the demo page to see its Redis-backed visit count increase. That is the
first proof that Orbit coordinated a host process with container
infrastructure and injected the runtime connection details. Afterward, run
`orbit open` to inspect and control the same environment in the dashboard.

When you need more detail:

```bash
orbit doctor             # explain unmet setup requirements
orbit logs demo-api -f   # stream the demo application log
orbit status --json      # stable machine-readable state
```

On Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.ps1 | iex
orbit init --yes
orbit up
orbit status
orbit open demo-api
```

The Unix export makes the newly installed command available immediately when
the installer uses `~/.local/bin`. Add the same line to your shell profile to
keep it available in future terminals. The Windows installer updates both the
current PowerShell process and the user PATH.

Upgrade, rollback, uninstall, manual downloads, and testing unreleased `main`
are documented in [Installation and development](docs/development.md).

From an Orbit source checkout, the
[mini-shop demo](docs/examples/mini-shop/README.md) provides a larger
`frontend + multiple backends + database/cache` example. Repository-relative
commands under `docs/examples/` are contributor examples; they are not part of
the installed-user path above.

`orbit init` accepts those distribution defaults without asking about an
environment repository or project workspace the demo does not use.
Orbit does not install project runtimes or dependencies implicitly;
`orbit doctor` reports what the selected environment expects and detects an
unsatisfied Python `requirements.txt` before startup. After `orbit up`, Orbit
shows the healthy application URL and one command to open it; the dashboard
remains a secondary option.

macOS and Linux are supported. Windows builds are Beta; see
[platform support and installation](docs/development.md#platform-support).

## Common workflows

```bash
orbit up                     # start services and their dependencies
orbit status --json          # inspect stable machine-readable state
orbit logs demo-api -f       # stream the default demo service
orbit env sync               # refresh and apply shared environment files
orbit switch quickstart      # select the default demo environment
orbit doctor --json          # diagnose the local setup
orbit down                   # stop the environment
```

`orbit up` is the normal start command. Use `orbit up --infra` only when you
intentionally want containers without host services. To narrow startup, choose
either resource names or one or more `--group` flags; Orbit rejects combinations
instead of silently ignoring part of the command.

For a team environment, replace `demo-api` and `quickstart` with names shown by
`orbit status` and `orbit env list`. After a switch, Orbit reports any runtime
version or project-package setup required before `orbit up`, using the
project's version files when present.

`orbit env sync` refreshes shared configuration and, when the active
environment changed, offers to make it current while restoring the resources
that were running. Resources that were already stopped stay stopped. For the
exceptional case where an interruption must be deferred, use `--no-apply`;
Orbit then prints the exact command to finish later.

For configured infrastructure containers, `orbit query redis`,
`orbit query mongo`, and `orbit query postgres` open the container's native
client. PostgreSQL uses the container's `POSTGRES_USER` and `POSTGRES_DB`;
pass `--database` only when you need another database. These query helpers are
separate from the optional SQL Server schema-project workflow below.

## Optional workflows

The related dashboard pages and setup checks stay hidden, and the commands are
inactive, unless the active environment explicitly enables the corresponding
extension. They are not required to use Orbit for host processes and
containers.

### SQL Server Database Projects

```bash
orbit db list
orbit db diff AppDB
orbit db publish AppDB
```

An environment opts in with a `sqlserver:` section. `publish` applies a schema
diff while preserving data; destructive `reset` and forced-publish paths
require confirmation. See [SQL Server workflow](docs/sql-workflow.md).

### Callback tunnels

```bash
orbit tunnel claim /callbacks/example -p 8080
```

Claim only authorized development or staging paths. Callback traffic may
contain credentials or personal data. See
[Tunnel claims](docs/tunnel-claim.md).

## Using Orbit with an AI agent

The repository ships `plugins/orbit-agent`, a version-matched Codex and Claude
plugin. Its operational skill teaches agents to inspect state first, use
`--json`, and confirm destructive operations.

Without installing the plugin, point an agent to
[the skill](plugins/orbit-agent/skills/orbit/SKILL.md) and
[the JSON contract](docs/agent-cli.md).

To use the bundled plugin directly from a source checkout, add
`plugins/orbit-agent` as a local Codex plugin or Claude Code plugin. The plugin
version always matches the Orbit release tag; see
[Versioning and compatibility](docs/versioning.md).

## Dashboard

Run `orbit open` after the daemon starts. The dashboard provides:

- dependency graph and service controls;
- environment preview and switching;
- logs, configuration, and health diagnostics;
- optional SQL Server schema inspection and publishing when enabled by the
  active environment;
- traces and request playback.

The dashboard listens locally at <http://localhost:19800>.

## Documentation

Core user documentation:

- [Configuration](docs/configuration.md)
- [Tracing](docs/tracing.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Versioning and compatibility](docs/versioning.md)

Optional workflows:

- [SQL Server Database Projects](docs/sql-workflow.md)
- [Tunnel claims](docs/tunnel-claim.md)

For adopters and contributors:

- [Team adoption](docs/team-adoption.md)
- [Architecture](docs/architecture.md)
- [Development](docs/development.md)
- [Agent CLI contract](docs/agent-cli.md)
- [Code conventions](docs/CODE_CONVENTIONS.md)

The repository-local `/orbit-review` skill reviews changes against these
conventions. Run `make preflight` before every commit.

## License

[MIT](LICENSE)
