---
name: orbit
description: Operate and diagnose local development environments with the Orbit CLI. Use whenever a task involves starting or stopping services or containers, checking environment status, switching or syncing environments, reading logs or traces, querying local infrastructure, publishing or resetting development databases, claiming callback tunnels, or troubleshooting an Orbit-managed workspace.
---

# Orbit

Use Orbit as the control plane for the local environment. Prefer its structured
CLI over invoking Docker, databases, or service processes directly.

## Core loop

1. Run `orbit inspect --json` once to get readiness, environment/resource
   state, risks, and recommended actions.
2. If blocked, follow the one applicable `recommended_actions` entry after
   checking its scope and `destructive` flag. Do not invent an intermediate
   diagnostic command.
3. Make the requested change with the narrowest lifecycle command.
4. Verify the result with `orbit status --json`.

Use `--json` whenever the command supports it. Human output is not a stable
parsing contract. `orbit up` starts the daemon when needed and safely applies
pending config edits; do not manage the daemon preemptively.

Agent and advanced commands such as `inspect`, `history`, and `env apply` are
intentionally omitted from contextual human help. Invoke them as documented;
do not treat absence from `orbit --help` as proof that a command is unavailable.

Read [references/workflows.md](references/workflows.md) for command selection
and destructive-operation rules. Read the repository's `docs/agent-cli.md`
when implementing or debugging an `orbit.cli.v1` consumer.

## State changes

- Use `orbit up --json` for the environment, or
  `orbit up <resource> --json` for one resource and its dependencies.
- Use `orbit restart <resource> --json` and
  `orbit down <resource> --json` for targeted lifecycle changes.
- Use `orbit up --infra --json` only when the user explicitly wants
  containers without host services.
- Use `orbit env sync --json` to refresh shared configuration and
  `orbit switch <name> --json` to select it.
- After editing the active config, normal `orbit up --json` validates before
  interruption, restores resources that were running, then performs the
  requested startup selection. Use `orbit env apply --json` when the edit must
  apply without starting any resource that was already stopped.

## Diagnostics

- Reuse the initial inspect snapshot; do not run inspect again inside the same
  recovery flow.
- Use `orbit doctor --json` only when a recommended action asks for it or when
  runtime/setup checks need more detail.
- Prefer `orbit logs <resource> --json` over reading process files directly;
  use `-f --json` only when an NDJSON stream is useful.
- Use history and trace commands only when the task needs correlation or an
  audit trail; they are not startup prerequisites.
- Treat the JSON envelope's final state and actions as authoritative rather
  than inferring success from exit code or transport state.

## Safety

- Ask before `orbit db reset` or `orbit db publish --force`; these may discard
  local data.
- Do not remove Docker volumes unless the user explicitly requests it.
- Do not edit `~/.orbit/settings.json` directly; use `orbit settings`,
  `orbit init`, or `orbit env sync`.
- Treat environment repositories as executable configuration. Inspect
  unfamiliar commands before starting them.
- Do not expose logs, environment variables, repository paths, or tunnel
  traffic outside the user's stated scope.
