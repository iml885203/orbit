# Local tracing

[English](./tracing.md) · [繁體中文](./tracing.zh-TW.md)

Follow a request across local services with OpenTelemetry traces in the
dashboard or CLI. Use its `TraceId` to find the logs for that request.

## On by default

Tracing is **on by default** — Orbit collects local traces with zero config. The
daemon runs an OTLP/HTTP receiver on `127.0.0.1:4318` and injects the standard
`OTEL_*` env vars into every dev service. Services with an OTLP exporter
configured to read those variables pick the receiver up automatically.

**Hybrid injection.** A service that already sets `OTEL_EXPORTER_OTLP_ENDPOINT`
itself (deliberately wired to an external collector) is left untouched — Orbit
stands aside rather than redirect its telemetry, and that service's spans won't
appear in Orbit. Every other service is pointed at Orbit's receiver.

**Turn it off per env** (rarely needed) by adding an explicit opt-out, then
running the normal start command again:

```yaml
tracing:
  enabled: false
```

```bash
orbit up
```

Orbit validates the edit before interrupting the running environment, applies
it, and restores the resources that were already running.

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
orbit trace --search item/1  # traces whose span names or attribute values match
orbit trace --service api    # traces that include that service
orbit trace --json           # structured list (for agents); with -f, NDJSON
orbit trace <id>             # ASCII waterfall of one trace
orbit trace <id> --logs      # waterfall + log lines and log records carrying this trace id
orbit trace <id> --json      # full span tree + per-span data
orbit trace logs             # OTLP log records (see Log records below)
orbit tracing status         # is the receiver healthy? which port? counters
```

When there are no traces to show, `orbit trace` explains why in terms of the
receiver's real state — off, on-but-the-receiver-didn't-bind, or on-and-idle —
so you can tell the three apart. `orbit tracing status` reports the same health
on demand (and as `--json` for scripts/agents).

Span labels are never clipped: the list prints the root span last, and the
waterfall prints each label after its bar.

### Health-probe traffic

Orbit probes each HTTP health check every few seconds, and an instrumented
service turns each probe into a trace. The probe sends the User-Agent
`orbit-health-probe`; a trace counts as health traffic when its root span
records that user agent, or — for instrumentation that does not record it —
when the root span's route or path equals that resource's `health_check.path`.
`orbit trace` and `GET /api/traces` hide such traces unless they errored, and
the human list says how many it hid; `--include-health` (`?include_health=1`)
shows them. The dashboard's list does not hide them yet.

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

## Log records

A service whose OpenTelemetry logging pipeline has an OTLP exporter sends its
log records to the same receiver (`/v1/logs`); it reads the endpoint Orbit
injects, so no extra configuration is needed. Orbit keeps each record's
severity, body, trace and span ids, scope, and attributes, including
`exception.type`, `exception.message` and `exception.stacktrace`. Records are
attributed to the resource's `service.name`, like spans. Metrics sent to
`/v1/metrics` are accepted and dropped.

`orbit trace logs` lists them, `orbit trace <id> --logs` shows the records of
one trace, and `orbit logs <resource>` is unchanged and still shows the
resource's stdout. Flags, the JSON page and `--since` paging are in
[agent-cli.md](agent-cli.md#log-records).

Records live in one in-memory ring shared by every service and are dropped on
`orbit down`. The ring is bounded by record count and by bytes, evicting the
oldest records past either bound. Very long body and attribute values are
truncated, and attributes past the OpenTelemetry default count limit are
dropped.

## Logs ↔ traces

Because Orbit owns both the trace store and the log stream, and the logs already
carry `TraceId`/`SpanId`, the join is **exact** — never a fuzzy timestamp match:

- A log line with a trace id shows a 🔍 action that opens its trace waterfall.
- A selected span's inspector shows only that span's log lines (by `SpanId`),
  falling back to the trace-level lines when the logger omits span ids.

## How it works

```
dev service ──OTLP/HTTP──▶ daemon receiver (:4318) ──▶ trace + log-record rings
                                                          │
   CLI (orbit trace)  ◀── unix socket ◀──────────────────┤
   dashboard          ◀── HTTP + SSE 'trace' event ◀──────┘
```

The receiver is HTTP-only; Orbit injects `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`
so services target it without Orbit needing a gRPC dependency. Spans are keyed by
trace id and accumulate across exports; summaries (root, duration, services,
status) are derived on read.
