import { describe, it, expect, beforeEach } from 'vitest'
import { liveTraffic } from './liveTraffic.svelte'

describe('liveTraffic', () => {
  beforeEach(() => {
    liveTraffic.enabled = false
    liveTraffic.clear()
  })

  it('is a no-op while disabled', () => {
    liveTraffic.note(['api', 'billing'])
    expect(liveTraffic.recentServices).toEqual([])
    expect(liveTraffic.isLive('api')).toBe(false)
  })

  it('marks services and edges live once enabled', () => {
    liveTraffic.enabled = true
    liveTraffic.note(['api', 'billing'])
    expect(liveTraffic.isLive('api')).toBe(true)
    expect(liveTraffic.isEdgeLive('api', 'billing')).toBe(true)
    expect(liveTraffic.isEdgeLive('api', 'catalog')).toBe(false)
  })

  it('unions a sliding window of recent traces', () => {
    liveTraffic.enabled = true
    liveTraffic.note(['api'])
    liveTraffic.note(['billing'])
    expect(liveTraffic.recentServices.sort()).toEqual(['api', 'billing'])
  })

  it('toggling off clears state', () => {
    liveTraffic.enabled = true
    liveTraffic.note(['api'])
    liveTraffic.toggle() // -> off
    expect(liveTraffic.enabled).toBe(false)
    expect(liveTraffic.recentServices).toEqual([])
    expect(liveTraffic.isLive('api')).toBe(false)
  })
})
