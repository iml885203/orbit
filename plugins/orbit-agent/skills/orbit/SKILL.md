---
name: orbit
description: Operate and diagnose local development environments with the Orbit CLI. Use whenever a task involves starting or stopping services or containers, checking environment status, switching or syncing environments, reading logs or traces, querying local infrastructure, publishing or resetting development databases, claiming callback tunnels, or troubleshooting an Orbit-managed workspace.
---

# Orbit

Use Orbit as the control plane for the local environment. Prefer its structured
CLI over invoking Docker, databases, or service processes directly.

## Start every task

1. Run `orbit status --json`.
2. If setup or configuration looks unhealthy, run `orbit doctor --json`.
3. Inspect the active environment before changing state.
4. Use `--json` for every command that supports it. Human output is not a
   stable parsing contract.

Read [references/workflows.md](references/workflows.md) for command selection
and destructive-operation rules. Read the repository's `docs/agent-cli.md`
when implementing or debugging an `orbit.cli.v1` consumer.

## State changes

- Use `orbit up --infra` for containers only and `orbit up` for services.
- Use `orbit service start|stop|restart <name> --json` for one service.
- Use `orbit env sync --json` to refresh shared configuration and
  `orbit switch <name> --json` to select it.
- Verify changes with `orbit status --json`; do not infer success solely from
  a zero exit code when the response provides structured state.

## Diagnostics

- Prefer `orbit logs <service>` over reading process files directly.
- Use `orbit inspect --json`, `orbit history --json`, and trace commands to
  correlate failures.
- Follow `recommended_actions` in JSON responses after confirming their
  destructive flag and scope.
- Use `orbit doctor --json` before changing user settings or installing tools.

## Safety

- Ask before `orbit db reset`, `orbit db publish --clean`, or
  `orbit db publish --force`; these may discard local data.
- Do not remove Docker volumes unless the user explicitly requests it.
- Do not edit `~/.orbit/settings.json` directly; use `orbit settings`,
  `orbit init`, or `orbit env sync`.
- Treat environment repositories as executable configuration. Inspect
  unfamiliar commands before starting them.
- Do not expose logs, environment variables, repository paths, or tunnel
  traffic outside the user's stated scope.
