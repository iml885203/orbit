import { describe, it, expect } from 'vitest'
import { extractTraceId, traceColor } from './traceColor'

describe('extractTraceId', () => {
  it('matches Serilog JSON-payload form', () => {
    const line = '[INF] foo {"TraceId": "d6193ddd6223189dbabdb8bb1119a4d9"}'
    expect(extractTraceId(line)).toBe('d6193ddd6223189dbabdb8bb1119a4d9')
  })

  it('matches key=value form', () => {
    const line = 'something happened TraceId=d6193ddd6223189dbabdb8bb1119a4d9 done'
    expect(extractTraceId(line)).toBe('d6193ddd6223189dbabdb8bb1119a4d9')
  })

  it('matches snake_case key', () => {
    const line = '[INF] foo {"trace_id": "abcdef1234567890abcdef1234567890"}'
    expect(extractTraceId(line)).toBe('abcdef1234567890abcdef1234567890')
  })

  it('matches 16-hex form too', () => {
    const line = '[INF] foo {"TraceId": "1234567890abcdef"}'
    expect(extractTraceId(line)).toBe('1234567890abcdef')
  })

  it('returns null when no trace id present', () => {
    expect(extractTraceId('[INF] just a normal line')).toBeNull()
  })

  it('returns null for non-hex string of right length', () => {
    expect(extractTraceId('TraceId=zzzzzzzzzzzzzzzz')).toBeNull()
  })

  it('returns the first match when multiple present', () => {
    const line = '"TraceId": "aaaaaaaaaaaaaaaa" "TraceId": "bbbbbbbbbbbbbbbb"'
    expect(extractTraceId(line)).toBe('aaaaaaaaaaaaaaaa')
  })
})

describe('traceColor', () => {
  it('returns the same color for the same id', () => {
    const id = 'd6193ddd6223189dbabdb8bb1119a4d9'
    expect(traceColor(id)).toBe(traceColor(id))
  })

  it('returns an hsl string', () => {
    const c = traceColor('any-id')
    expect(c).toMatch(/^hsl\(\d+(?:\.\d+)?, \d+%, \d+%\)$/)
  })

  it('hue stays inside the safe band (90..340)', () => {
    // Sample many ids and confirm none land in the red/orange/yellow
    // hue range (0–89 or 341–360). 200 samples is plenty.
    for (let i = 0; i < 200; i++) {
      const c = traceColor(`trace-${i}`)
      const m = /^hsl\((\d+)/.exec(c)
      expect(m).not.toBeNull()
      const hue = Number(m![1])
      expect(hue).toBeGreaterThanOrEqual(90)
      expect(hue).toBeLessThanOrEqual(340)
    }
  })

  it('different ids land on different hues', () => {
    const colors = new Set([
      traceColor('d6193ddd6223189dbabdb8bb1119a4d9'),
      traceColor('1234567890abcdef1234567890abcdef'),
      traceColor('feedfacedeadbeef0123456789abcdef'),
      traceColor('00000000000000000000000000000001'),
    ])
    // With 250° of hue space, four random hashes essentially never
    // collide. Asserting >= 3 distinct catches a degenerate "all same
    // color" implementation while tolerating the rare 1-in-thousands
    // legitimate collision.
    expect(colors.size).toBeGreaterThanOrEqual(3)
  })
})
