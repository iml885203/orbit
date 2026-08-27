# ![Orbit](ui/public/orbit-logo-badge.svg) Orbit

**Build the product. Let your agent run the project.**

Orbit gives coding agents a reliable way to get the local environment a
project needs running, understand failures, and verify that the application
actually works.

[Official website](https://orbit.dotw.me/) · [Documentation](#documentation) ·
[繁體中文](README.zh-TW.md)

## Get started

### Give your agent one request

Paste this into your coding agent from the project you want to run:

```text
Read https://orbit.dotw.me and help me get this project running with Orbit.
You may install the official Orbit CLI and plugin. Ask before installing other
software or making destructive changes.
```

The agent inspects the existing setup, starts the intended environment,
verifies a real application flow, and reports anything that still needs you.

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
- [Agent instructions](https://orbit.dotw.me/agent/SKILL.md)
- [Contributing](CONTRIBUTING.md)
- [Documentation website maintenance](docs/website.md)

## License

[MIT](https://github.com/iml885203/orbit/blob/main/LICENSE). The binary embeds
third-party dependencies; their licenses and attributions are listed in
[NOTICE](https://github.com/iml885203/orbit/blob/main/NOTICE).
