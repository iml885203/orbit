// Trace ID extraction + deterministic color assignment.
//
// Application logs (Serilog JSON payload) include "TraceId": "<32-hex>".
// Some loggers use TraceId=<32-hex> or trace_id=<...>. Match both shapes.
//
// Color assignment is a pure hash → palette lookup so the same TraceId
// always lands on the same color across modal opens, page reloads, and
// browser sessions.

const TRACE_ID_PATTERNS = [
  /"(?:TraceId|trace_id|traceId)"\s*:\s*"([0-9a-fA-F]{16,32})"/,
  /\b(?:TraceId|trace_id|traceId)=([0-9a-fA-F]{16,32})\b/,
]

export function extractTraceId(line: string): string | null {
  for (const re of TRACE_ID_PATTERNS) {
    const m = re.exec(line)
    if (m) return m[1]
  }
  return null
}

// SpanId follows the same logger conventions as TraceId but is 16 hex chars
// (8 bytes). Used for the exact span↔log join in the trace inspector; absent
// in many log lines, in which case the line is treated as trace-level only.
const SPAN_ID_PATTERNS = [
  /"(?:SpanId|span_id|spanId)"\s*:\s*"([0-9a-fA-F]{16})"/,
  /\b(?:SpanId|span_id|spanId)=([0-9a-fA-F]{16})\b/,
]

export function extractSpanId(line: string): string | null {
  for (const re of SPAN_ID_PATTERNS) {
    const m = re.exec(line)
    if (m) return m[1]
  }
  return null
}

// Trace color = HSL hue derived from a hash of the TraceId, restricted
// to a "safe" hue band that avoids red (~0°/360°), orange (~30°), and
// yellow (~50°) so trace bars never look like the error/warn row tints.
//
// Safe band: 90° (yellow-green / cyan-leaning) → 340° (just before red).
// That's 250° of usable hue space, ~250 distinguishable trace colors —
// vastly more than a fixed 8-slot palette, with collisions only when
// two TraceIds hash within ~2° of each other.
//
// Saturation and lightness are fixed so every trace has the same
// visual weight — only the hue varies.
const HUE_OFFSET = 90       // start of safe band (degrees)
const SAFE_HUE_RANGE = 250  // 90 .. 340, skipping the red→yellow arc
const SATURATION = 70       // %
const LIGHTNESS = 65        // %

// FNV-1a 32-bit hash. Fast, deterministic, no dependencies.
function fnv1a(s: string): number {
  let h = 0x811c9dc5
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 0x01000193)
  }
  return h >>> 0
}

export function traceColor(traceId: string): string {
  const hue = HUE_OFFSET + (fnv1a(traceId) % SAFE_HUE_RANGE)
  return `hsl(${hue}, ${SATURATION}%, ${LIGHTNESS}%)`
}
