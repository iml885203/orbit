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

Traces live in memory (default 1000 traces) and are cleared by `orbit down`.
The oldest whole trace is evicted when either the count or approximate 64 MiB
retention budget is reached. This budget covers retained data, not process RSS.
Each trace holds at most 2000 spans. Re-exporting a span replaces its previous
value rather than consuming another slot.

| Input | Limit and result |
|---|---|
| One traces/logs export | 8 MiB compressed and decoded; excess returns HTTP 413 before ingestion |
| Trace name, status message, kind/status | 16 KiB content; UTF-8-safe truncation with `…[truncated]` |
| Trace/span/parent IDs and service name | 1 KiB each; an oversized identity rejects the span rather than merging different identities |
| Span attributes | 16 KiB total and 128 attributes; an oversized set is omitted |

`orbit tracing status --json` reports retained bytes, the budget, whole-trace
evictions and rejected spans. Body rejections are HTTP 413, not an invented
span count. The per-minute rate uses fixed one-second buckets and expires while
idle. These fixed limits require no YAML changes.

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

Trace detail merges bursts of live updates into a single refresh sequence. Switching
traces or closing the dialog cancels its pending requests; late results cannot
replace another trace or its logs.

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

Records share one in-memory ring: at most 10,000 records and approximately
32 MiB, with oldest records evicted first. `orbit down` clears it. Body,
severity, scope and attribute values retain at most 16 KiB content plus a
`…[truncated]` marker. Identity fields use the same 1 KiB rejection limit as
spans. Attribute keys over 16 KiB are omitted; at most 128 attributes survive
export conversion (direct ingest sets over that count are omitted).

## Stdout history

Stdout keeps the same log/history and follow formats. Very long lines are emitted
in 64 KiB content chunks with ` [line continues]`; a huge line written directly
to history is truncated with ` [line truncated]`. Markers are additional bytes.
This keeps draining producers even when a line never ends.

History keeps at most 1000 lines and 1 MiB per service, and 16 MiB across services.
It retains at most 128 service names of up to 256 bytes; longer names are rejected.
The oldest lines go first within a service; aggregate pressure evicts the least
recently written service's whole history. Follow continues delivering new output
when older history is evicted. Internal retention counters distinguish eviction,
truncation and rejected service lines. Capture durable logs separately when you
need a complete history.

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
