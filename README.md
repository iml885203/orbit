# <img src="ui/public/orbit-logo-badge.svg" width="32" height="32" alt=""> Orbit

Run a mixed local-development stack—host processes and containers—from one
declarative environment.

[Install](docs/development.md) · [Configuration](docs/configuration.md) ·
[Architecture](docs/architecture.md) · [Agent CLI](docs/agent-cli.md) ·
[繁體中文](README.zh-TW.md)

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
- **Optional workflows:** publish SQL schema changes without rebuilding an
  image, and route an authorized callback path to a local service.

See [Why Orbit](docs/why-orbit.md) for design trade-offs and comparisons.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh | bash
orbit init
```

Upgrade, rollback, uninstall, manual downloads, and source builds are documented
in [Installation and development](docs/development.md).

The default setup uses the
[Orbit demo environment](https://github.com/iml885203/orbit-demo): a
standard-library Python service running on the host with Redis in a container.
Orbit does not install Python or other project runtimes; `orbit doctor` reports
what the selected environment expects.

## Common workflows

```bash
orbit up --infra             # start containers only
orbit up                     # start services and their dependencies
orbit status --json          # inspect stable machine-readable state
orbit logs api -f            # stream one service
orbit env sync --json        # refresh shared environment files
orbit switch development     # select an environment
orbit doctor --json          # diagnose the local setup
orbit down                   # stop the environment
```

### Database workflow

```bash
orbit db diff AppDB
orbit db publish AppDB
orbit db reset AppDB         # destructive: discards local data
```

`publish` applies a schema diff while preserving data. `reset` restores a clean
development database and requires confirmation. See
[SQL workflow](docs/sql-workflow.md).

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
`--json`, and confirm destructive database operations.

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
- local database change inspection and publishing;
- traces and request playback.

The dashboard listens locally at <http://localhost:19800>.

## Documentation

For users:

- [Configuration](docs/configuration.md)
- [SQL workflow](docs/sql-workflow.md)
- [Tracing](docs/tracing.md)
- [Tunnel claims](docs/tunnel-claim.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Versioning and compatibility](docs/versioning.md)

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
