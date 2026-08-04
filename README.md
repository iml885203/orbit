# <img src="ui/public/orbit-logo-badge.svg" width="32" height="32" alt=""> Orbit

Run a mixed stack—host processes and containers—from one declarative
environment, for local development and E2E testing.

[Install](docs/development.md) · [Configuration](docs/configuration.md) ·
[Environment sources](docs/environment-sources.md) · [Architecture](docs/architecture.md) · [Agent CLI](docs/agent-cli.md) ·
[繁體中文](README.zh-TW.md)

> **Pre-1.0 preview:** Orbit is open for early adopters and contributors while
> the 1.0 UX and compatibility contracts are still being finalized. Expect
> breaking changes before `v1.0.0`.

Orbit starts services in dependency order, checks health, streams logs, and
keeps the environment alive behind a resident daemon. People drive it from the
CLI and dashboard; coding agents drive it through the stable `orbit.cli.v1`
JSON contract. The same environment serves daily development and acts as the
substrate under an E2E suite—on your machine or a CI runner.

- **Mixed runtimes:** coordinate host processes and containers in one graph.
- **Shared environments:** sync versioned YAML from any Git repository.
- **Agent-first control:** coding agents drive the full lifecycle through the
  versioned `orbit.cli.v1` JSON contract—stable error codes and executable
  recommended actions.
- **E2E substrate:** a test suite verifies the shared environment instead of
  provisioning its own copy—see
  [Using Orbit under an E2E test suite](docs/e2e-testing.md).
- **Local diagnostics:** health, logs, history, traces, and configuration are
  visible from the CLI and dashboard.

See [Why Orbit](docs/why-orbit.md) for the design trade-offs behind these
choices.

## One file describes the environment

Save an `orbit.yaml` beside your code
([runnable example](docs/examples/local-first)):

```yaml
version: "3"

containers:
  redis:
    image: redis:7.4-alpine
    ports:
      redis: "26379:6379"

services:
  app:
    kind: frontend
    command: python3 -m http.server "$PORT"
    ports:
      http: 28080
    depends_on: [redis]
```

The daily loop is four commands:

```bash
orbit up       # start everything in dependency order
orbit status   # see what is actually ready
orbit logs app # inspect application output
orbit down     # stop the environment
```

Orbit starts Redis before the host process, waits for real readiness instead
of equating "process exists" with "ready", and injects the declared port as
`PORT`. The default runtime keeps ports fixed: a conflict is reported with its
owning process and the remedy. After editing `orbit.yaml`, run `orbit up`
again: the new config is validated before anything is interrupted.

For parallel local checkouts or CI jobs, select a named instance. Named
instances isolate daemon state, Docker resources, volumes, networks, and host
ports while leaving the unnamed runtime backward compatible:

```bash
orbit up --instance checkout-a
orbit status --instance checkout-a --json
orbit instance list --json
orbit instance clean checkout-a
```

Declared host ports are preferences inside a named instance. Orbit persists
the resolved ports for stable restarts and reports the actual endpoints in
`up`, `status`, and `instance list`; callers do not need to coordinate
`ORBIT_HOME`, `ORBIT_NAMESPACE`, `ORBIT_DASHBOARD_PORT`, or `ORBIT_SOCKET`.
See [Isolated runtime instances](docs/instances.md) for targeting, isolation,
port resolution, and cleanup semantics.

[Use Orbit with your project](docs/local-first.md) walks this path and shows
how to promote the proven file into a shared team environment. Every field is
documented in [Configuration](docs/configuration.md).

## Install

macOS or Linux with Homebrew:

```bash
brew install iml885203/tap/orbit
```

Windows with Scoop (Beta):

```powershell
scoop bucket add iml885203 https://github.com/iml885203/scoop-bucket
scoop install orbit
```

Or use the direct verified installer on macOS and Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh | bash
export PATH="$HOME/.local/bin:$PATH"
```

Windows PowerShell (Beta):

```powershell
irm https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.ps1 | iex
```

This installs the latest published preview release. Orbit installs only its
own CLI—never a project's runtimes or dependencies; `orbit doctor` reports
what the selected environment expects and the specific remedy. Upgrade,
rollback, uninstall, and platform details are in
[Installation and development](docs/development.md).

## Try the demo

The public demo needs Git, Docker, and Python 3:

```bash
git clone https://github.com/iml885203/orbit-demo.git
cd orbit-demo
orbit up
orbit status
orbit open demo-shop
```

![Orbit demo dashboard showing the mini-shop dependency graph](docs/assets/orbit-demo-dashboard.jpg)

The [Orbit demo](https://github.com/iml885203/orbit-demo) is a standalone
mini-shop — a storefront, three Python APIs with SQLite databases on the
host, and Redis in a container — driven by one project-root `orbit.yaml`,
exactly how Orbit works in a real project. In the storefront, **Run
checkout** shows one request crossing catalog, inventory, Redis, and orders;
**Try 99 items** shows the failure path commit no order and leave stock
unchanged. Stop everything with `orbit down`. The same application also runs
without Orbit (`./scripts/run-local.sh`), which makes visible exactly what
Orbit takes off your hands.

## Using Orbit with an AI agent

Agents read state through the same CLI with `--json`; errors carry stable
codes and executable recommended actions:

```bash
orbit status --json
orbit doctor --json
orbit env info --json   # ports, URLs, and credentials-by-reference for anything living beside the stack
```

The repository ships `plugins/orbit-agent`, a version-matched Claude and
Codex plugin whose skill teaches agents to inspect state first, prefer
`--json`, and confirm destructive operations. In Claude Code:

```bash
claude plugin marketplace add iml885203/orbit
claude plugin install orbit-agent@orbit
```

Without the plugin, point an agent to
[the skill](plugins/orbit-agent/skills/orbit/SKILL.md) and
[the JSON contract](docs/agent-cli.md).

## Dashboard

`orbit open` opens the local dashboard: the dependency graph with service
controls, environment preview and switching, logs, health diagnostics, traces,
and request playback. It binds to loopback only.

## Documentation

Core user documentation:

- [Use Orbit with your project](docs/local-first.md)
- [Why Orbit](docs/why-orbit.md) — design trade-offs and tool comparisons
- [Configuration](docs/configuration.md)
- [Environment sources](docs/environment-sources.md)
- [Isolated runtime instances](docs/instances.md)
- [Using Orbit under an E2E test suite](docs/e2e-testing.md)
- [Tracing](docs/tracing.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Versioning and compatibility](docs/versioning.md)

Optional workflows, inactive unless the environment enables them:

- [SQL Server Database Projects](docs/sql-workflow.md)
- [Tunnel claims](docs/tunnel-claim.md)

For adopters and contributors:

- [Team adoption](docs/team-adoption.md)
- [Architecture](docs/architecture.md)
- [Development](docs/development.md)
- [Agent CLI contract](docs/agent-cli.md)
- [Contributing](CONTRIBUTING.md)

## License

[MIT](LICENSE). The binary embeds third-party dependencies; their licenses and
attributions are listed in [NOTICE](NOTICE).
