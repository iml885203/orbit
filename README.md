# ![Orbit](ui/public/orbit-logo-badge.svg) Orbit

**Build the product. Let agents run the environment.**

Orbit is an agent-native local development orchestrator. It gives developers
and coding agents one reliable interface to set up, run, inspect, and manage a
project's entire local stack.

[Official website](https://iml885203.github.io/orbit/) · [Try the demo](#try-orbit) ·
[Use Orbit with your project](docs/local-first.md) · [Install](docs/development.md) ·
[Documentation](#documentation) ·
[繁體中文](README.zh-TW.md)

![Orbit dashboard showing a healthy mini-shop dependency graph](docs/assets/orbit-demo-dashboard.jpg)

Local setup should not live in a developer's head. Orbit turns one
`orbit.yaml` into an executable environment contract shared by developers,
CI, and coding agents.

- **Encode the environment once:** dependencies, commands, ports, and readiness
  live in one versioned definition instead of setup notes and tribal knowledge.
- **Delegate with confidence:** the stable JSON CLI gives agents observable
  state, structured errors, and safe next actions instead of brittle shell
  automation.
- **Keep humans focused:** Orbit coordinates host processes and containers and
  surfaces health, logs, ports, traces, and failures in one place.

## Try Orbit

The demo needs Git, Docker, Python 3, and an
[installed Orbit CLI](docs/development.md):

```bash
git clone https://github.com/iml885203/orbit-demo.git
cd orbit-demo
orbit up
orbit status
orbit open demo-shop
```

The [Orbit demo](https://github.com/iml885203/orbit-demo) is a tiny storefront:
three host APIs, live stock in a Redis container, and orders in SQLite. Buy a
mug to see one request cross the whole graph, then use `orbit down` to stop it.

## One file describes the environment

Save an `orbit.yaml` beside your code
([runnable example](https://github.com/iml885203/orbit/tree/main/docs/examples/local-first)):

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

## Using Orbit with an AI agent

Agents read state through the same CLI with `--json`; errors carry stable
codes and executable recommended actions:

```bash
orbit inspect --json       # one runtime-readiness decision and safe next actions
orbit status --json        # selected environment and current resource states
orbit doctor --json        # host prerequisites and setup diagnostics
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
[the skill](https://github.com/iml885203/orbit/blob/main/plugins/orbit-agent/skills/orbit/SKILL.md) and
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
- [Documentation website maintenance](docs/website.md)

## License

[MIT](https://github.com/iml885203/orbit/blob/main/LICENSE). The binary embeds
third-party dependencies; their licenses and attributions are listed in
[NOTICE](https://github.com/iml885203/orbit/blob/main/NOTICE).
