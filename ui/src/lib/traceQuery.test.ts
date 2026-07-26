import { describe, it, expect } from 'vitest'
import { parseTraceQuery, buildTraceQuery } from './traceQuery'

describe('traceQuery', () => {
  it('round-trips filters through the query string', () => {
    const f = { errored: true, minDurationMs: 200, search: 'api /api' }
    expect(parseTraceQuery(buildTraceQuery(f))).toEqual(f)
  })

  it('returns an empty string when every filter is default', () => {
    expect(buildTraceQuery({ errored: false, minDurationMs: 0, search: '' })).toBe('')
  })

  it('parses defaults from an empty or unrelated query', () => {
    expect(parseTraceQuery('')).toEqual({ errored: false, minDurationMs: 0, search: '' })
    expect(parseTraceQuery('foo=bar')).toEqual({ errored: false, minDurationMs: 0, search: '' })
  })

  it('handles values containing = and & (the hand-rolled parser bug)', () => {
    const f = { errored: false, minDurationMs: 0, search: 'a=b&c' }
    expect(parseTraceQuery(buildTraceQuery(f)).search).toBe('a=b&c')
  })
})
