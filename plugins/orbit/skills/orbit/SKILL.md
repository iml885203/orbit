---
name: orbit
description: Operate and diagnose local development environments with the Orbit CLI. Use whenever a task involves installing or setting up Orbit for the first time, starting or stopping services or containers, checking environment status, switching or syncing environments, reading logs or traces, querying local infrastructure, publishing or resetting development databases, claiming callback tunnels, or troubleshooting an Orbit-managed workspace.
---

# Orbit

Use Orbit as the control plane for the local environment. Prefer its structured
CLI over invoking Docker, databases, or service processes directly.

## Setup

If `orbit` is not installed, identify the host platform, explain the matching
official installer, and get the user's approval before running it. On macOS or
Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh | bash
```

On Windows PowerShell (Beta):

```powershell
irm https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.ps1 | iex
```

Read the installer's reported `Installed:` path and verify the result with
`orbit version --json`, invoking that exact binary path when needed. Agent
shell calls may not preserve PATH changes; if a later call cannot find `orbit`,
invoke the installed path or prefix that call with the path the installer
reported. Do not install or update Docker, package managers, or project
runtimes without the user's approval. Docker must be running when the selected
environment declares containers; the public demo does.

Then pick the path that matches the user:

- **They explicitly asked for the public demo** — work from a new empty
  directory with no ancestor `orbit.yaml`. Run `orbit inspect --json`, follow
  its exact non-destructive `orbit init --yes --json` action, and continue the
  core loop. Do not present the other setup paths unless the request is
  ambiguous.
- **Their project already has `orbit.yaml`** — nothing to set up. Run
  `orbit up` from anywhere inside the project; Orbit finds the nearest config.
- **They have a team environment repository** —
  `orbit init --yes --source <name> --url <url> [--env <name>]` configures a named source,
  syncs the repository, and selects an environment.
- **They have neither** — run `orbit inspect --json` and follow its exact setup
  action. The public demo is the fastest way to show what Orbit does. For a
  real project, prefer a project-local `orbit.yaml` over building an
  environment repository.

If a command reports that Orbit is not set up, follow its stated next step
rather than guessing between these paths.

## Core loop

1. Run `orbit inspect --json` once to get readiness, environment/resource
   state, risks, and recommended actions.
2. If blocked, follow the one applicable `recommended_actions` entry after
   checking its scope and `destructive` flag. Do not invent an intermediate
   diagnostic command.
3. Make the requested change with the narrowest lifecycle command.
4. Verify the result with `orbit status --json`.

If the user names an instance, or the task runs beside another checkout or CI
job, choose one instance name before the first inspect and pass
`--instance <name>` to every targeted command. Never fall back to the default
runtime midway through a workflow. JSON recommended actions already retain the
active instance target.

Use `--json` whenever the command supports it. Human output is not a stable
parsing contract. `orbit up` starts the daemon when needed and safely applies
pending config edits; do not manage the daemon preemptively.

Orbit ships commands faster than this file can track it, so treat
`orbit <command> --help` as the current truth. Absence from `orbit --help` is
not proof a command is unavailable: agent and advanced commands such as
`inspect`, `history`, and `env apply` are intentionally hidden from contextual
human help. Invoke them as documented.

Read [references/workflows.md](references/workflows.md) for command selection
and destructive-operation rules. Deeper references live in the repository's
`docs/`: `agent-cli` (the `orbit.cli.v1` contract), `instances`, `configuration`,
`troubleshooting`, `tracing`, `sql-workflow`, `architecture`.

## State changes

- Use `orbit up --json` for the environment, or
  `orbit up <resource> --json` for one resource and its dependencies.
- Use `orbit restart <resource> --json` and
  `orbit down <resource> --json` (or multiple resource names), `--group`, or `--infra` for targeted
  lifecycle changes; `up` and `down` use the same selection modes.
- Use `orbit up --infra --json` only when the user explicitly wants
  containers without host services.
- Use `orbit instance list --json` to discover named runtimes and their actual
  endpoints. Named-instance ports may differ from the declarations.
- Use `orbit source sync [<name>] --json` to refresh shared configuration and
  `orbit switch <name> --json` to select it. With resources running, switch
  returns the stable `confirmation_required` error instead of acting; run the
  recommended `--yes` command only when the user intends to stop that stack.
- Use `orbit env info --json` to learn how to reach the env's resources:
  ports and URLs carry `declared` vs `observed` provenance, and observed
  values are withheld when the daemon serves a different environment.
  Environment values need `--show-secrets`; key names are always listed.
- After editing the active config, normal `orbit up --json` validates before
  interruption, restores resources that were running, then performs the
  requested startup selection. Use `orbit env apply --json` when the edit must
  apply without starting any resource that was already stopped.

## Diagnostics

- Reuse the initial inspect snapshot; do not run inspect again inside the same
  recovery flow.
- Use `orbit doctor --json` only when a recommended action asks for it or when
  runtime/setup checks need more detail.
- Treat `dependency_readiness_ambiguous` as a configuration-authoring risk,
  not proof that startup failed. Tell the user which `health_check` path Orbit
  could not infer; do not invent a probe or edit project intent without scope.
- When `config_invalid` includes a `did you mean` correction, preserve that
  exact field or value in the explanation. Apply it only when the task includes
  editing the environment; otherwise report the correction without mutating
  project intent.
- Prefer `orbit logs <resource> --json` over reading process files directly;
  use `-f --json` only when an NDJSON stream is useful.
- Use history and trace commands only when the task needs correlation or an
  audit trail; they are not startup prerequisites.
- Treat the JSON envelope's final state and actions as authoritative rather
  than inferring success from exit code or transport state.

## Safety

- Ask before `orbit sqlserver reset` or `orbit sqlserver publish --allow-data-loss`; these may discard
  local data.
- Treat `orbit switch` as destructive when resources are running: it stops
  them. Never pass `--yes` on the user's behalf without their intent.
- Treat `orbit instance clean <name>` as destructive to that instance's local
  processes, state, containers, and volumes. Run it only when the user's task
  includes disposing of that instance.
- Do not remove Docker volumes unless the user explicitly requests it.
- Do not edit `~/.orbit/settings.json` directly; use `orbit settings`,
  `orbit init`, or `orbit source sync`.
- Treat environment repositories as executable configuration. Inspect
  unfamiliar commands before starting them.
- Do not expose logs, environment variables, repository paths, or tunnel
  traffic outside the user's stated scope.
