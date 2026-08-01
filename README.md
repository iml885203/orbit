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

The public demo requires:

- [Git](https://git-scm.com/downloads) to sync its environment;
- [Docker](https://docs.docker.com/get-docker/) for Redis; and
- [Python 3](https://www.python.org/downloads/) for its host-side services.

Orbit installs only its own CLI. It detects project runtimes from the selected
environment and gives the specific remedy before starting anything; it never
installs or changes a project's toolchain implicitly.

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
orbit open demo-shop
```

`orbit init --yes` uses the public
[Orbit demo environment](https://github.com/iml885203/orbit-demo): a storefront,
three standard-library Python APIs and their SQLite databases on the host, plus
Redis in a container. If a required tool is unavailable, setup stops before
startup and prints the specific remedy. Occupied preferred ports are replaced
automatically across the dependency graph.

Choose **Run checkout**. The page shows one request cross catalog, inventory,
Redis, and orders while preserving the links between product, reservation, and
order. **Try 99 items** proves the failure path commits no order and leaves
stock unchanged: it shows `7 → 7`, `+0` reservations, and `+0` orders while
keeping the earlier successful order visible. This is direct evidence that
Orbit coordinated a useful mixed runtime application—not only that five
processes started. Afterward, run `orbit open` to inspect and control the same
environment in the dashboard. When you finish the trial, stop everything with:

```bash
orbit down
```

Ready to try Orbit on a real checkout?
[Start with one project-local `orbit.yaml`](docs/local-first.md). The
ten-minute path needs no environment repository, no `orbit init`, and no
manual `~/.orbit` settings. A host service starts with its command and port;
Orbit infers common runtimes and uses the config directory as its working
directory. The guide also shows when and how to promote the proven file into a
shared team environment.

When you need more detail:

```bash
orbit doctor             # explain unmet setup requirements
orbit logs shop-order-api -f # stream the checkout path
orbit status --json      # stable machine-readable state
```

On Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.ps1 | iex
orbit init --yes
orbit up
orbit status
orbit open demo-shop
```

The Unix export makes the newly installed command available immediately when
the installer uses `~/.local/bin`. Add the same line to your shell profile to
keep it available in future terminals. The Windows installer updates both the
current PowerShell process and the user PATH.

Upgrade, rollback, uninstall, manual downloads, and testing unreleased `main`
are documented in [Installation and development](docs/development.md).

For a larger environment after the five-minute trial,
[the extended mini-shop](https://github.com/iml885203/orbit-examples/tree/main/mini-shop)
provides an independent eight-API project. Clone it separately and use the
same project-local `orbit up` workflow; complete application examples do not
live in the Orbit source repository.

`orbit init` accepts those distribution defaults without asking about an
environment repository or project workspace the demo does not use.
Orbit does not install project runtimes or dependencies implicitly;
`orbit doctor` reports what the selected environment expects and detects an
unsatisfied Python `requirements.txt`, missing Node packages, and missing host
runtimes such as Go before startup. After `orbit up`, Orbit shows the healthy
application URL and one command to open it; the dashboard remains a secondary
option.

macOS and Linux are supported. Windows builds are Beta; see
[platform support and installation](docs/development.md#platform-support).

## Common workflows

```bash
orbit up                     # start services and their dependencies
orbit status --json          # inspect stable machine-readable state
orbit logs shop-order-api -f # stream the default checkout path
orbit env sync               # refresh and apply shared environment files
orbit switch quickstart      # select the default demo environment
orbit doctor --json          # diagnose the local setup
orbit down                   # stop the environment
```

`orbit up` is the normal start command. Use `orbit up --infra` only when you
intentionally want containers without host services. To narrow startup, choose
either resource names or one or more `--group` flags; Orbit rejects combinations
instead of silently ignoring part of the command.

`orbit down` uses the same selectors: no selector stops the environment,
resource names stop only those resources, `--group` stops the selected groups,
and `--infra` stops only containers. You do not need a second selection model
for shutdown.

After editing the active `orbit.yaml`, run `orbit up` again. Orbit validates the
new config before interrupting anything, applies it, restores the resources that
were already running, and then starts the resources requested by the command.
Use `orbit env apply` only when you want to apply pending changes without
starting additional stopped resources.

For a team environment, replace `demo-shop` and `quickstart` with names shown by
`orbit status` and `orbit env list`. After a switch, Orbit reports any runtime
version or project-package setup required before `orbit up`, using the
project's version files when present.

`orbit env sync` refreshes shared configuration and, when the active
environment changed, offers to make it current while restoring the resources
that were running. Resources that were already stopped stay stopped. For the
exceptional case where an interruption must be deferred, use `--no-apply`;
Orbit then prints the exact command to finish later. The official demo is
pinned to the revision shipped with each Orbit release; `orbit env list` and
`orbit status` show its repository, requested ref, and resolved commit. Teams
can get the same reproducibility with
`orbit init --env-repo <url> --env-ref <tag-or-commit>`.

For configured infrastructure containers, `orbit query redis`,
`orbit query mongo`, and `orbit query postgres` open the container's native
client. PostgreSQL uses the container's `POSTGRES_USER` and `POSTGRES_DB`;
pass `--database` only when you need another database. With one matching
container Orbit selects it; with several, it lists the candidates and requires
`--container <name>` instead of guessing. These query helpers are separate from
the optional SQL Server schema-project workflow below.

## Optional workflows

The related dashboard pages and setup checks stay hidden, and the commands are
inactive, unless the active environment explicitly enables the corresponding
extension. They are not required to use Orbit for host processes and
containers.

### SQL Server Database Projects

```bash
orbit sqlserver list
orbit sqlserver diff AppDB
orbit sqlserver publish AppDB
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

In Claude Code, install it from this repository:

```bash
claude plugin marketplace add iml885203/orbit
claude plugin install orbit-agent@orbit
```

For Codex, or from a source checkout, add `plugins/orbit-agent` as a local
plugin. The plugin version always matches the Orbit release tag; see
[Versioning and compatibility](docs/versioning.md).

## Dashboard

Run `orbit open` after the daemon starts. The dashboard provides:

- dependency graph and service controls;
- environment preview and switching;
- logs, configuration, and health diagnostics;
- optional SQL Server schema inspection and publishing when enabled by the
  active environment;
- traces and request playback.

The dashboard normally listens locally at <http://localhost:19800>. If that
port is already in use, Orbit selects another available port automatically;
`orbit open` always opens the active address.

## Documentation

Core user documentation:

- [Use Orbit with your project](docs/local-first.md)
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

[MIT](LICENSE). The binary embeds third-party dependencies; their licenses and
attributions are listed in [NOTICE](NOTICE).
