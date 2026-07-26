import { describe, it, expect, beforeEach } from 'vitest'
import { playback } from './tracePlayback.svelte'
import type { Trace, Span } from './types.gen'

function span(spanId: string, parentId: string, service: string, status: string, startMs: number): Span {
  return {
    traceId: 't1', spanId, parentId, service, name: service + '-op',
    startUnixNano: startMs * 1e6, endUnixNano: (startMs + 10) * 1e6, durationMs: 10, status,
  }
}

const trace: Trace = {
  traceId: 't1', rootService: 'api', rootName: 'POST /pay',
  startUnixNano: 0, durationMs: 100, spanCount: 3, services: ['api', 'billing', 'catalog'], status: 'error',
  spans: [
    span('a', '', 'api', 'ok', 0),
    span('b', 'a', 'billing', 'error', 10),
    span('c', 'a', 'catalog', 'ok', 20),
  ],
}

describe('tracePlayback.start', () => {
  beforeEach(() => playback.exit())

  it('derives services in entry order, hops, and failed services', () => {
    playback.start(trace)
    expect(playback.active).toBe(true)
    expect(playback.services).toEqual(['api', 'billing', 'catalog'])
    expect(playback.steps).toEqual([
      { from: 'api', to: 'billing', error: true },
      { from: 'api', to: 'catalog', error: false },
    ])
    expect(playback.failed).toEqual(['billing'])
    // starts fully revealed
    expect(playback.current).toBe(1)
  })

  it('lights services/edges only up to the current step', () => {
    playback.start(trace)
    playback.prev() // current = 0 → only first hop revealed
    expect(playback.current).toBe(0)
    expect(playback.isServiceActive('api')).toBe(true)
    expect(playback.isServiceActive('billing')).toBe(true)
    expect(playback.isServiceActive('catalog')).toBe(false)
    expect(playback.isEdgeActive('api', 'billing')).toBe(true)
    expect(playback.isEdgeActive('api', 'catalog')).toBe(false)

    playback.prev() // current = -1 → only root
    expect(playback.isServiceActive('api')).toBe(true)
    expect(playback.isServiceActive('billing')).toBe(false)
  })

  it('clamps stepping and resets on exit', () => {
    playback.start(trace)
    playback.next() // already at last
    expect(playback.current).toBe(1)
    expect(playback.serviceFailed('billing')).toBe(true)
    expect(playback.inTrace('catalog')).toBe(true)
    playback.exit()
    expect(playback.active).toBe(false)
    expect(playback.services).toEqual([])
    expect(playback.isServiceActive('api')).toBe(false)
  })
})
