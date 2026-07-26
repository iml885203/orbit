// Pure layout + formatting helpers for the trace waterfall. No store access,
// no Svelte — just span math so it stays unit-testable and shared by the
// table, waterfall, and inspector.

import type { Span, Trace } from './types.gen'
import { extractSpanId } from './traceColor'

export function formatDurationMs(ms: number): string {
  if (ms <= 0) return '0ms'
  if (ms < 1) return '<1ms'
  if (ms < 1000) return Math.round(ms) + 'ms'
  return (ms / 1000).toFixed(2) + 's'
}

export function formatClock(unixNano: number): string {
  if (!unixNano) return '--:--:--'
  return new Date(unixNano / 1e6).toLocaleTimeString(undefined, { hour12: false })
}

// spanDepths computes each span's indentation depth by walking parent links.
// A span whose parent is absent from the set is a root (depth 0). A visited
// guard bounds pathological cycles.
export function spanDepths(spans: Span[]): Record<string, number> {
  const byId = new Map<string, Span>()
  for (const s of spans) byId.set(s.spanId, s)
  const depth: Record<string, number> = {}
  for (const s of spans) {
    let d = 0
    let cur: Span | undefined = s
    const seen = new Set<string>()
    while (cur && cur.parentId && !seen.has(cur.spanId)) {
      seen.add(cur.spanId)
      const parent = byId.get(cur.parentId)
      if (!parent) break
      d++
      cur = parent
    }
    depth[s.spanId] = d
  }
  return depth
}

export type SpanLayout = { offsetPct: number; widthPct: number }

// spanLayout positions a span's bar relative to the trace's own start and
// duration. This is a relative scale for spotting bottlenecks, not an absolute
// axis. Widths clamp to a visible minimum and never overflow the track.
export function spanLayout(span: Span, traceStartNano: number, traceDurationMs: number): SpanLayout {
  const total = traceDurationMs > 0 ? traceDurationMs : 1
  const startMs = (span.startUnixNano - traceStartNano) / 1e6
  let offset = (startMs / total) * 100
  let width = (span.durationMs / total) * 100
  offset = Math.min(Math.max(offset, 0), 100)
  if (width < 0.8) width = 0.8
  if (offset + width > 100) width = 100 - offset
  return { offsetPct: offset, widthPct: width }
}

// selectSpanLogs picks the log lines for one span out of a service's lines
// (already filtered to the trace id). This is integration ②, the exact
// span↔log join: when the logger emits SpanId, keep only the lines whose span
// id matches; when it doesn't (no line carries a SpanId), fall back to the
// trace-level set so the inspector is never empty just because the format
// omits span ids. Never guesses by timestamp.
export function selectSpanLogs(traceLevelLines: string[], spanId: string): string[] {
  const anyHasSpanId = traceLevelLines.some((l) => extractSpanId(l) !== null)
  if (!anyHasSpanId) return traceLevelLines
  return traceLevelLines.filter((l) => extractSpanId(l) === spanId)
}

// traceToText renders a trace as a pasteable, human-readable block (header +
// indented span tree) for the "Copy trace" action.
export function traceToText(trace: Trace): string {
  const depths = spanDepths(trace.spans)
  const out: string[] = [
    `trace ${trace.traceId}`,
    `${trace.rootService} ${trace.rootName}  ${formatDurationMs(trace.durationMs)}  ${trace.status}  ${trace.spanCount} spans`,
  ]
  for (const s of trace.spans) {
    const indent = '  '.repeat(depths[s.spanId] ?? 0)
    out.push(`${indent}${s.service} ${s.name}  ${formatDurationMs(s.durationMs)}${s.status === 'error' ? '  ✗' : ''}`)
  }
  return out.join('\n')
}
