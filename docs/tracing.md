# Local tracing

[English](./tracing.md) · [繁體中文](./tracing.zh-TW.md)

Orbit captures OpenTelemetry traces from locally instrumented services and
shows them next to the graph and logs you already use, without a separate trace
UI.

A trace is a path through the dependency graph; Orbit's logs already carry the
`TraceId`. Tracing is the connective tissue between them.

## On by default

Tracing is **on by default** — Orbit collects local traces with zero config, the
same way the .NET Aspire dashboard does. The daemon runs an OTLP/HTTP receiver on
`127.0.0.1:4318` and injects the standard `OTEL_*` env vars into every dev
service. Services with an OTLP exporter configured to read those variables pick
the receiver up automatically.

**Hybrid injection.** A service that already sets `OTEL_EXPORTER_OTLP_ENDPOINT`
itself (deliberately wired to an external collector) is left untouched — Orbit
stands aside rather than redirect its telemetry, and that service's spans won't
appear in Orbit. Every other service is pointed at Orbit's receiver.

**Turn it off per env** (rarely needed) by adding an explicit opt-out to the env
YAML, then restarting the daemon:

```yaml
tracing:
  enabled: false
```

```bash
orbit down && orbit up   # restart the daemon to apply
```

See [configuration.md](configuration.md#tracing) for the knobs (`otlp_port`,
`max_traces`).

Traces live in an in-memory ring buffer (default 1000) and are dropped on
`orbit down` — this is a local dev aid, not a retention store. Ingest is bounded
on three axes so a busy service can't grow memory without limit: the trace count
(`max_traces`), spans per trace, and attribute bytes per span. Spans shed by
these ceilings are counted and reported by `orbit tracing status`.

If the configured port is already taken, Orbit auto-advances to the next free
port (unless you pinned `otlp_port`, in which case a conflict is reported rather
than silently moved). `orbit tracing status` shows the port actually in use.

## CLI

```bash
orbit trace                  # recent traces, newest first
orbit trace -f               # stream new traces live as they arrive
orbit trace --json           # structured list (for agents); with -f, NDJSON
orbit trace <id>             # ASCII waterfall of one trace
orbit trace <id> --logs      # waterfall + log lines carrying this trace id
orbit trace <id> --json      # full span tree + per-span data
orbit tracing status         # is the receiver healthy? which port? counters
```

When there are no traces to show, `orbit trace` explains why in terms of the
receiver's real state — off, on-but-the-receiver-didn't-bind, or on-and-idle —
so you can tell the three apart. `orbit tracing status` reports the same health
on demand (and as `--json` for scripts/agents).

There is intentionally no `enable`/`disable` command: tracing is on by default,
and opting a single env out is a one-line `enabled: false` edit — the same
mental model as every other env setting.

## Dashboard

Open the **Tracing** tab (`http://localhost:19800/#/tracing`):

- **List** — newest-first, with a live spans/min indicator, and
  errored / min-duration / search filters that sync to the URL.
- **Detail** (`#/tracing/:traceId`) — a span waterfall (bars coloured by service
  kind; errors marked) plus a span inspector showing attributes and the log
  lines for that span, joined by exact span/trace id.
  - **Open all logs** opens the log viewer filtered to the whole trace.
  - Selecting a span scrolls to and flashes its lines in the synced trace-log
    panel below the waterfall.
  - **⧉ Copy** copies the trace (a pasteable span tree), its logs, or both —
    handy for filing a bug or handing context to an agent. Log lines also have
    per-line 📋 / whole-trace 🧵 copy actions.
  - **Play on graph** replays the request across the Services graph: each hop
    lights up in order and a failed service pulses red. Step through with the
    playback bar.

On the Services graph, the **Live** toggle pulses the flow-dot along edges that
recently carried a trace — an ambient "the system is moving" view, separate from
single-trace playback.

## Logs ↔ traces

Because Orbit owns both the trace store and the log stream, and the logs already
carry `TraceId`/`SpanId`, the join is **exact** — never a fuzzy timestamp match:

- A log line with a trace id shows a 🔍 action that opens its trace waterfall.
- A selected span's inspector shows only that span's log lines (by `SpanId`),
  falling back to the trace-level lines when the logger omits span ids.

## How it works

```
dev service ──OTLP/HTTP──▶ daemon receiver (:4318) ──▶ ring buffer
                                                          │
   CLI (orbit trace)  ◀── unix socket ◀──────────────────┤
   dashboard          ◀── HTTP + SSE 'trace' event ◀──────┘
```

The receiver is HTTP-only; Orbit injects `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`
so services target it without Orbit needing a gRPC dependency. Spans are keyed by
trace id and accumulate across exports; summaries (root, duration, services,
status) are derived on read.
