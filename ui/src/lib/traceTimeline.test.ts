import { describe, it, expect } from 'vitest'
import { spanDepths, spanLayout, formatDurationMs, selectSpanLogs, traceToText } from './traceTimeline'
import type { Span, Trace } from './types.gen'

function span(spanId: string, parentId: string, startMs: number, durMs: number): Span {
  return {
    traceId: 't', spanId, parentId, service: 'svc', name: 'n',
    startUnixNano: startMs * 1e6, endUnixNano: (startMs + durMs) * 1e6,
    durationMs: durMs, status: 'ok',
  }
}

describe('spanDepths', () => {
  it('nests by parent links', () => {
    const spans = [span('a', '', 0, 100), span('b', 'a', 10, 50), span('c', 'b', 20, 10)]
    const d = spanDepths(spans)
    expect(d.a).toBe(0)
    expect(d.b).toBe(1)
    expect(d.c).toBe(2)
  })
  it('treats a span with an absent parent as a root', () => {
    const d = spanDepths([span('x', 'missing', 0, 10)])
    expect(d.x).toBe(0)
  })
  it('does not loop on a cycle', () => {
    const a = span('a', 'b', 0, 10)
    const b = span('b', 'a', 0, 10)
    const d = spanDepths([a, b])
    expect(Number.isFinite(d.a)).toBe(true)
    expect(Number.isFinite(d.b)).toBe(true)
  })
})

describe('spanLayout', () => {
  it('positions a span relative to trace start/duration', () => {
    const s = span('b', 'a', 100, 200) // starts 100ms in, lasts 200ms
    const l = spanLayout(s, 0, 400)
    expect(l.offsetPct).toBeCloseTo(25)
    expect(l.widthPct).toBeCloseTo(50)
  })
  it('clamps a tiny span to a visible minimum and never overflows', () => {
    const s = span('b', 'a', 399, 0.0001)
    const l = spanLayout(s, 0, 400)
    expect(l.widthPct).toBeGreaterThan(0)
    expect(l.offsetPct + l.widthPct).toBeLessThanOrEqual(100.001)
  })
})

describe('selectSpanLogs', () => {
  const a = 'lvl=INFO SpanId=1111111111111111 hello'
  const b = 'lvl=INFO SpanId=2222222222222222 world'
  const noSpan = 'lvl=INFO TraceId=abc only-trace'

  it('keeps only lines whose span id matches when span ids are present', () => {
    expect(selectSpanLogs([a, b], '1111111111111111')).toEqual([a])
  })
  it('falls back to all trace-level lines when no line carries a span id', () => {
    expect(selectSpanLogs([noSpan, noSpan], 'whatever')).toEqual([noSpan, noSpan])
  })
  it('never timestamp-guesses: a non-matching span with span ids present yields nothing', () => {
    expect(selectSpanLogs([a, b], '9999999999999999')).toEqual([])
  })
})

describe('traceToText', () => {
  it('renders a header and an indented span tree with an error marker', () => {
    const trace: Trace = {
      traceId: 'abc', rootService: 'api', rootName: 'POST /pay',
      startUnixNano: 0, durationMs: 100, spanCount: 2, services: ['api', 'billing'], status: 'error',
      spans: [span('a', '', 0, 100), { ...span('b', 'a', 10, 50), service: 'billing', name: 'settle', status: 'error' }],
    }
    const text = traceToText(trace)
    expect(text).toContain('trace abc')
    expect(text).toContain('api POST /pay  100ms  error  2 spans')
    // child is indented and flagged
    expect(text).toMatch(/\n {2}billing settle {2}50ms {2}✗/)
  })
})

describe('formatDurationMs', () => {
  it('formats ms and seconds', () => {
    expect(formatDurationMs(412)).toBe('412ms')
    expect(formatDurationMs(1200)).toBe('1.20s')
    expect(formatDurationMs(0)).toBe('0ms')
    expect(formatDurationMs(0.3)).toBe('<1ms')
  })
})
