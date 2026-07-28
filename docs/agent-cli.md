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
    "code": "unknown_service",
    "message": "unknown service: redisx",
    "hint": "Run orbit status --json to list known services.",
    "retryable": false,
    "next_command": "orbit status --json"
  },
  "recommended_actions": [
    {
      "command": "orbit status --json",
      "reason": "List known services and current states."
    }
  ]
}
```

Agents should prefer `error.next_command` and `recommended_actions` over
guessing a recovery path from the message text.

## Converted Commands

These commands currently use the `orbit.cli.v1` envelope when `--json` is set:

| Command | JSON behavior |
|---|---|
| `orbit version --json` | Returns the installed Orbit version. |
| `orbit doctor --json` | Returns diagnostic checks in `data`. |
| `orbit inspect --json` | Returns an agent-ready state snapshot with readiness, daemon/env summaries, service risks, and recommended follow-up commands. |
| `orbit status --json` | Returns setup readiness, daemon state, and configured service state in `data`. |
| `orbit logs <service> --json` | Returns recent log lines in one JSON object. |
| `orbit logs <service> -f --json` | Streams NDJSON events, one JSON object per line. |
| `orbit up --json` | Returns the resources actually selected by the daemon (including dependencies and group filtering), observed final states, degraded/timed-out services, and recommended follow-up commands. An environment with no enabled resources succeeds immediately with empty arrays. |
| `orbit down --json` | Returns final lifecycle result after stopping services. |
| `orbit down <service> --json` | Returns final lifecycle result for the requested service. |
| `orbit restart --json` | Returns final lifecycle result and verifies restart evidence. |
| `orbit env use <env> --json` | Returns the selected env, env name, daemon running state, and whether restart is required. |
| `orbit env sync --json` | Returns sync source, destination, dry-run state, written files, daemon running state, and restart recommendation. |
| `orbit switch <env> --json` | Returns the selected env, daemon start/restart action, final daemon state, config path, dashboard URL, and the new env's prerequisite checks/readiness. |
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

`data.requested_services` on `up` is the daemon-resolved start set, despite the
legacy field name. It includes selected dependencies and excludes resources
filtered out by groups. This is the exact set whose terminal state the command
waits for.

Before first setup, status remains a successful state query with
`data.setup_required: true`, a human-readable `setup_message`, and exactly one
`orbit init --yes --json` action. A present but invalid environment file is
instead an `invalid_environment` error, because startup cannot safely use it.

Stable `data.operation` values for converted control commands:

| Command | `data.operation` |
|---|---|
| `orbit env use <env> --json` | `env_use` |
| `orbit env sync --json` | `env_sync` |
| `orbit switch <env> --json` | `switch` |
| `orbit daemon start --json` | `daemon_start` |
| `orbit daemon stop --json` | `daemon_stop` |
| `orbit daemon restart --json` | `daemon_restart` |
| `orbit uninstall --json` | `uninstall` |

For `switch`, `daemon_running_before` and `daemon_running_after` describe the
daemon transition. `prerequisites_ready` is false when the newly selected env
is missing a required runtime or package installation; `prerequisites` carries
the same checks as Doctor, and `recommended_actions` includes exact runnable
remedies when Orbit can determine one.

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
`orbit.cli.v1` envelope, with `data.schema_version` set to `orbit.inspect.v1`.

The inspect payload contains:

| Field | Meaning |
|---|---|
| `readiness` | Stable decision state with `state`, `blocked`, and `summary`. |
| `daemon` | Daemon running state, PID, version, upgrade info, and dashboard URL when available. |
| `env` | Current config path, env name, preview-only flag, and daemon-reported env when available. |
| `services` | Bounded service summary grouped by state. |
| `risks` | Ordered machine-readable risks such as `setup_required`, `config_invalid`, `environment_stopped`, `env_mismatch`, `status_unavailable`, `service_degraded`, `service_converging`, and `service_stopped`. |
| `recommended_actions` | Safe next commands the agent should consider. |

Stable `readiness.state` values:

| State | Blocked | Meaning |
|---|---:|---|
| `setup_required` | true | No usable environment has been selected; the only next action is `orbit init --yes --json`. |
| `config_invalid` | true | The selected config cannot be loaded. |
| `stopped` | true | The selected environment is configured but not running; configured resources are listed as stopped and the only action is `orbit up --json`. |
| `needs_daemon` | true | A running daemon is serving a different env than the selected config. |
| `degraded` | false | At least one service reports `degraded`. |
| `converging` | false | At least one service is `pending`, `starting`, `building`, `stopping`, or `restarting`. |
| `partial` | false | At least one service is stopped and no higher-priority risk exists. |
| `ready` | false | No inspect risk was detected. |

`converging` may also be returned when daemon service status is temporarily
unavailable. In that case, a `status_unavailable` risk explains the condition.

Agents should treat `blocked: true` as a stop-and-recover state. Agents may run
non-destructive `recommended_actions` directly, but must still require explicit
human approval for any action marked `destructive: true`.

When readiness is `needs_daemon`, agents should run the recommended
`orbit switch <env> --json` action before acting on service state because the
running environment differs from the selected CLI config.

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
orbit logs <service> --json
orbit restart <service> --json  # after fixing the reported cause
```

If a JSON response includes `recommended_actions`, follow those before falling
back to ad hoc debugging.

## Exit Codes

| Code | Meaning |
|---|---|
| `0` | Success. |
| `1` | Error. In `--json` mode, converted commands still emit structured JSON. |
