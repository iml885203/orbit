# Agent CLI Contract

[English](./agent-cli.md) · [繁體中文](./agent-cli.zh-TW.md)

Orbit is designed to be driven by coding agents through the CLI. Agents should
prefer structured output over human-formatted text whenever they need to parse
state, decide next actions, or recover from errors.

## Rule of Thumb

Use `--json` for programmatic reads and writes:

```bash
orbit status --json
orbit doctor --json
orbit up --infra --json
orbit logs redis --json
```

Human output is optimized for terminals and may change. JSON output is the
agent-facing contract.

## JSON Envelope

Commands converted to the agent contract return one JSON object with this
top-level shape:

```json
{
  "schema_version": "orbit.cli.v1",
  "ok": true,
  "command": "doctor",
  "data": {},
  "error": null,
  "recommended_actions": []
}
```

Fields:

| Field | Meaning |
|---|---|
| `schema_version` | Contract version. Current value is `orbit.cli.v1`. |
| `ok` | `true` on success, `false` on failure. |
| `command` | The Orbit command that produced the response. |
| `data` | Command-specific payload on success. |
| `error` | Structured error payload on failure, otherwise `null`. |
| `recommended_actions` | Follow-up commands the agent should consider. |

When a converted command fails with `--json`, Orbit prints a single JSON object
to stdout and exits with code `1`.

## Error Shape

Structured errors use this shape:

```json
{
  "schema_version": "orbit.cli.v1",
  "ok": false,
  "command": "logs",
  "data": null,
  "error": {
    "code": "unknown_resource",
    "message": "unknown resource: redisx",
    "hint": "Run orbit status --json to list configured resources.",
    "retryable": false,
    "next_command": "orbit status --json"
  },
  "recommended_actions": [
    {
      "command": "orbit status --json",
      "reason": "List configured resources and current states."
    }
  ]
}
```

Agents should prefer `error.next_command` and `recommended_actions` over
guessing a recovery path from the message text.

For `resource_port_conflict`, `error.next_command` is the platform-specific
read-only command that inspects the current port owner. The resource's status
also includes `port_conflict` evidence (`port`, `resource`, optional owner, and
`inspect_command`). Do not retry or fetch logs until the owner is stopped or
the shared environment selects a different host port.

Resource status includes `logs_available: true` only when the running daemon
has buffered output for that resource. Absence means there is no historical
output to inspect yet; clients should not present Logs as a recovery action for
a dependency-blocked resource unless this evidence is true. A direct
`orbit logs <resource> --json` request for a resource that never started returns
`logs_unavailable`, not an empty success. Its sole recommended action is based
on a live port recheck: inspect the current owner while occupied, or retry only
that resource after the port is released.

A degraded host process may include both `state_reason` (for example,
`exited: exit status 1`) and `failure_evidence`, the last meaningful
application log line captured for that failed generation. Treat the evidence
as supporting detail, not a replacement for the lifecycle reason.

## Converted Commands

These commands currently use the `orbit.cli.v1` envelope when `--json` is set:

| Command | JSON behavior |
|---|---|
| `orbit version --json` | Returns the installed Orbit version. |
| `orbit doctor --json` | Returns diagnostic checks in `data`. |
| `orbit inspect --json` | Returns an agent-ready state snapshot with readiness, daemon/env summaries, resource risks, and recommended follow-up commands. |
| `orbit status --json` | Returns setup/selection readiness, the selected and available environments, managed repository URL/ref/commit when applicable, daemon state, and configured resource state in `data.resources`. |
| `orbit logs <resource> --json` | Returns recent log lines in one JSON object. |
| `orbit logs <resource> -f --json` | Streams NDJSON events, one JSON object per line. |
| `orbit up --json` | Returns the resources actually selected by the daemon (including dependencies and group filtering), observed final states, degraded/timed-out resources, and recommended follow-up commands. When it applies pending config edits, `data.environment_changes` reports running intent preserved across the handoff. An environment with no enabled resources succeeds immediately with empty arrays. |
| `orbit down --json` | Returns final lifecycle result after stopping resources. It is a successful no-op with empty arrays when Orbit is already stopped, and recommends only the next normal `orbit up`. |
| `orbit down <resource> --json` | Returns the final lifecycle result for the requested resource. |
| `orbit restart --json` | Returns final lifecycle result and verifies restart evidence. |
| `orbit env list --json` | Returns `data.environment` with the selection state, prior selection when unavailable, exact available environment choices, and managed repository URL/ref/commit when applicable. |
| `orbit env use <env> --json` | Returns the selected env, env name, daemon running state, and whether restart is required. |
| `orbit env sync --json` | Returns sync source, requested reference, resolved commit, destination, dry-run state, written files, daemon state, apply action, and restored resources. |
| `orbit env apply --json` | Applies pending environment changes, then returns the resources that were running, restored, or removed from the new config. |
| `orbit switch <env> --json` | Returns the selected env, daemon start/restart action, final daemon state, config path, dashboard URL, and the new env's prerequisite checks/readiness. |
| `orbit update --json` | Updates the invoked binary and, when an environment is running, reconnects it and returns the resources restored across the handoff. `--rollback` applies the same contract to the previous binary. |
| `orbit daemon start --json` | Returns daemon running state, PID, config path, and dashboard URL. |
| `orbit daemon stop --json` | Returns stopped state, previous PID, and whether service shutdown was requested. |
| `orbit daemon restart --json` | Returns previous/new daemon state, PID, config path, dashboard URL, and service shutdown effect. |
| `orbit uninstall --json` | Previews binary artifacts and whether user data is preserved; `--yes` is required before removal. |
| `orbit trace --json` | Returns recent trace summaries in `data.traces`, newest first. |
| `orbit trace -f --json` | Streams NDJSON trace-summary events, one JSON object per line. |
| `orbit trace <id> --json` | Returns one full trace (summary fields + `spans`) in `data`. |
| `orbit tracing status --json` | Returns the receiver's health, the port in use, and ingest counters in `data`. |

Lifecycle commands suppress decorative progress output in JSON mode so stdout
remains parseable.

Lifecycle actions are outcome-specific. A successful `up` returns one primary
next action, `orbit open --json`. A failed start recommends status, logs for the
root failed resource, and a targeted restart after the reported cause is fixed;
it does not send agents through unrelated setup diagnostics.

Lifecycle payloads use resource vocabulary consistently:

- `requested_resources` is the daemon-resolved set, including selected
  dependencies and excluding resources filtered out by groups.
- `resources` contains the observed final state for that set.
- `degraded_resources` and `timed_out_resources` identify unsuccessful
  outcomes without treating containers as services.
- `environment_changes`, when present, contains `previously_running`,
  `restored_resources`, `started_dependencies`, and `unavailable_resources`
  from the config handoff performed before startup.

This is the exact set whose terminal state the command waits for. Status and
inspect likewise expose mixed host processes and containers under `resources`.
Log payloads and NDJSON events identify their source with `resource`.

When a command discovers the nearest project `orbit.yaml`, status reports that
actual active config as `data.environment.source: "project"`, with
`selected_path` pointing to the discovered file. This project context takes
precedence over a managed environment selected elsewhere. Agents should use
the reported source and path rather than infer the active config from
`~/.orbit/envs`.

If another project-local environment is running, status remains successful and
describes only the current project's configured resources. It sets
`data.environment.context_switch_required: true`, reports `running_name` and
`running_path`, and recommends exactly `orbit up --json`. Doctor validates the
current project and treats ports owned by the running project as releasable
during that switch. Operational commands that could control the other
project's resources fail with `project_context_inactive`; agents should follow
its sole `orbit up --json` action. A successful switch reports
`data.context_switch` in the `up` result, including both project names and the
resources stopped before startup.
Inspect follows the same ownership boundary and never reports the other
project's resources or dashboard as belonging to the current project.

Before first setup, status remains a successful state query with
`data.setup_required: true`, a human-readable `setup_message`, and exactly one
`orbit init --yes --json` action. A present but invalid environment file is
instead an `invalid_environment` error, because startup cannot safely use it.
If the previously selected environment was renamed or removed, status instead
returns `data.selection_required: true`, a `selection_message`, and exact
`orbit switch <env> --json` choices; this does not mean setup must be repeated.

`orbit init --yes --json` never invents `data.workspace_root` from the current
directory. The field is omitted for self-contained environments. If a custom
environment requires `${WORKSPACE_ROOT}` and no proven local workspace exists,
init returns `service_working_directory_missing` with the sole action
`orbit settings set workspace-root "$PWD" --json`.
Other unresolved path variables preserve their name and lead to
`orbit settings set-env <NAME> "$PWD" --json`; they never produce a
workspace-root action.

When GitHub reports that an environment repository was not found, Orbit returns
`env_repo_unavailable` without recommended actions. GitHub deliberately uses
the same response for a misspelled or missing repository and a private
repository hidden from the current credentials, so an agent must not
automatically log in or retry unchanged. Verify the owner and repository name
first; authenticate Git only after confirming that the URL is correct and
private. Unambiguous authentication failures remain `env_repo_access`.

Stable `data.operation` values for converted control commands:

| Command | `data.operation` |
|---|---|
| `orbit env list --json` | `env_list` |
| `orbit env use <env> --json` | `env_use` |
| `orbit env sync --json` | `env_sync` |
| `orbit env apply --json` | `env_apply` |
| `orbit switch <env> --json` | `switch` |
| `orbit daemon start --json` | `daemon_start` |
| `orbit daemon stop --json` | `daemon_stop` |
| `orbit daemon restart --json` | `daemon_restart` |
| `orbit update --json` | `update` |
| `orbit update --rollback --json` | `rollback` |
| `orbit settings set <key> <value> --json` | `settings_set` |
| `orbit settings set-env <name> <value> --json` | `settings_set_env` |
| `orbit settings list --json` | `settings_list` |
| `orbit uninstall --json` | `uninstall` |

For `switch`, `previous_environment_stopped` says whether Orbit stopped a
previously running environment before making the selection. A stopped Orbit is
not started merely to record a selection; `orbit up` remains the sole startup
action. `prerequisites_ready` is false when the newly selected env is missing a
required runtime or package installation; `prerequisites` carries the same
checks as Doctor, and `recommended_actions` includes exact runnable remedies
when Orbit can determine one.

An unresolved or missing host-service path uses the stable
`service_working_directory_missing` error code. `switch` leaves the daemon
stopped, and Doctor or `up` returns the exact workspace-setting or config-edit
action. Execute that action before retrying; `up` has not started dependencies.

Daemon stop/restart payloads may include `stop_method` when a prior daemon was
running. Stable values are `graceful`, `terminated`, and `killed`.

## Legacy JSON Commands

Some commands predate the envelope and intentionally keep their existing JSON
shape for compatibility:

| Command | Behavior |
|---|---|
| `orbit daemon status --json` | Returns the legacy daemon status object. |
| `orbit history --json` | Keeps its existing history payload. |
| `orbit history gaps --json` | Keeps its existing history gaps payload. |

Do not assume every `--json` command has an envelope. Check the command-specific
contract before parsing.

The daemon HTTP API is structured for the Orbit UI and CLI internals, but the
current agent-facing compatibility contract is the CLI `--json` surface unless
a future design explicitly says otherwise.

## Logs

Non-streaming logs return one envelope:

```bash
orbit logs redis --json
```

The success payload includes the service name, requested line count, a non-null
`lines` array, and whether output was truncated.

Streaming logs use newline-delimited JSON:

```bash
orbit logs redis -f --json
```

Each emitted line is a complete JSON object. Agents should parse it as NDJSON,
not as one JSON array.

If a followed log stream fails after NDJSON output has begun, Orbit emits a
final NDJSON event with `type: "error"` instead of appending an envelope in a
different format, then exits non-zero.

## Traces

Tracing is on by default; it is off only when the active env sets an explicit
`tracing.enabled: false` (see [tracing.md](tracing.md)). Check the live receiver
state with `orbit tracing status --json`.

```bash
orbit trace --json             # recent traces; --limit N caps the count
orbit trace <trace-id> --json  # one full trace with its span tree
orbit trace -f --json          # NDJSON stream of trace-summary updates
```

A trace summary carries `traceId`, `rootService`, `rootName`, `startUnixNano`,
`durationMs`, `spanCount`, `services` (distinct, first-seen order), and
`status` (`ok` | `error`). The full trace adds `spans`, each with
`traceId`/`spanId`/`parentId`, `service`, `name`, `kind`, `startUnixNano`/
`endUnixNano`/`durationMs`, `status`/`statusMsg`, and `attributes`. These key
names are pinned by contract tests (`app/traces_json_test.go`) — treat
renames as breaking.

In `-f --json` mode the same `traceId` may be emitted repeatedly as its trace
accumulates spans; the latest event supersedes earlier ones.

A typical agent workflow: `orbit trace --json` → pick the errored trace →
`orbit trace <id> --json` to find the failing span → the trace's log lines via
`GET /api/traces/{id}/logs` on the daemon, or `orbit trace <id> --logs` for
human-readable output.

## Inspect

`orbit inspect --json` is the recommended first command for agents that need to
understand whether Orbit is ready for automation. It returns the normal
`orbit.cli.v1` envelope, with `data.schema_version` set to `orbit.inspect.v2`.

The inspect payload contains:

| Field | Meaning |
|---|---|
| `readiness` | Stable decision state with `state`, `blocked`, and `summary`. |
| `daemon` | Daemon running state, PID, version, upgrade info, and dashboard URL when available. |
| `environment` | The same `state`, `selected_name`, `selected_path`, and `environments` selection object used by status and env list, plus preview/daemon details when available. |
| `resources` | Bounded resource summary grouped by state. |
| `risks` | Ordered machine-readable risks such as `setup_required`, `environment_selection_required`, `orbit_update_pending`, `config_invalid`, `environment_stopped`, `env_mismatch`, `status_unavailable`, `resource_degraded`, `resource_converging`, and `resource_stopped`. |
| `recommended_actions` | Safe next commands the agent should consider. |

Stable `readiness.state` values:

| State | Blocked | Meaning |
|---|---:|---|
| `setup_required` | true | No usable environment has been selected; the only next action is `orbit init --yes --json`. |
| `selection_required` | true | The previous selection is unavailable. Actions contain exact `orbit switch <env> --json` choices, or `orbit env sync --json` when none are available. |
| `config_invalid` | true | The selected config cannot be loaded. An older shared schema has the sole action `orbit env sync --json`; a newer schema has the sole action `orbit update --json`. Syntax errors and unknown fields require editing the reported file, so Orbit does not return an unchanged `inspect` self-loop. |
| `update_required` | true | A newer Orbit binary is installed but the daemon still runs the previous version; the only action is `orbit daemon restart --json`. |
| `stopped` | true | The selected environment is configured but not running; configured resources are listed as stopped and the only action is `orbit up --json`. |
| `needs_daemon` | true | A running daemon is serving a different env than the selected config. |
| `degraded` | false | At least one resource reports `degraded`. |
| `converging` | false | At least one resource is `pending`, `starting`, `building`, `stopping`, or `restarting`. |
| `partial` | false | At least one resource is stopped and no higher-priority risk exists. |
| `ready` | false | No inspect risk was detected. |

For a terminal degraded resource with buffered output, `inspect`, `status`, and
`doctor` lead only to `orbit logs <resource> --json`. After returning the exit
output, `logs` rechecks live state. If a supported project dependency check is
failing, its sole action installs the declared dependencies and then restarts
only that resource; otherwise the sole action is
`orbit restart <resource> --json`. Follow that sequence instead of skipping
ahead: a newly observed port, dependency, or runtime cause can replace a blind
restart with its safer cause-specific action.

`converging` may also be returned when daemon resource status is temporarily
unavailable. In that case, a `status_unavailable` risk explains the condition.

Agents should treat `blocked: true` as a stop-and-recover state. Agents may run
non-destructive `recommended_actions` directly, but must still require explicit
human approval for any action marked `destructive: true`.

When readiness is `needs_daemon`, agents should run the recommended
`orbit switch <env> --json` action before acting on service state because the
running environment differs from the selected CLI config.

When readiness is `update_required`, agents must restart Orbit before sending
resource mutations. Orbit returns `orbit_update_pending` from those mutations
and recommends only `orbit daemon restart --json`.

## Recommended Agent Workflow

Start with inspect, act, then inspect again:

```bash
orbit inspect --json
# run the response's first non-destructive recommended action
orbit inspect --json
```

For a failing service:

```bash
orbit status --json
orbit logs <resource> --json
orbit restart <resource> --json  # after fixing the reported cause
```

If a JSON response includes `recommended_actions`, follow those before falling
back to ad hoc debugging.

## Exit Codes

| Code | Meaning |
|---|---|
| `0` | Success. |
| `1` | Error. In `--json` mode, converted commands still emit structured JSON. |
