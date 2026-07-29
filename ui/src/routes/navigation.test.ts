import { beforeEach, describe, expect, it } from 'vitest'
import { navItems, routes } from './index'
import { tracing } from '$lib/tracing.svelte'

describe('primary navigation', () => {
  beforeEach(() => tracing.reset())

  it('keeps diagnostics routable without making it a primary workspace', () => {
    expect(navItems.map((item) => item.label)).not.toContain('Health Check')
    expect(Object.prototype.hasOwnProperty.call(routes, '/healthcheck')).toBe(true)
  })

  it('reveals tracing only when it has value or needs recovery', () => {
    const tracingItem = navItems.find(item => item.label === 'Tracing')
    expect(tracingItem?.hidden?.()).toBe(true)

    tracing.status = {
      configured: true,
      receiverHealthy: true,
      otlpPort: 4318,
      actualPort: 4318,
      traceCount: 0,
      totalSpans: 0,
      spansPerMin: 0,
      lastReceivedUnixMs: 0,
      spansDropped: 0,
    }
    expect(tracingItem?.hidden?.()).toBe(true)

    tracing.upsert({
      traceId: 'trace-1',
      rootService: 'storefront',
      rootName: 'GET /',
      startUnixNano: 1,
      durationMs: 10,
      spanCount: 1,
      services: ['storefront'],
      status: 'ok',
    })
    expect(tracingItem?.hidden?.()).toBe(false)

    tracing.reset()
    tracing.status = {
      configured: true,
      receiverHealthy: false,
      otlpPort: 4318,
      actualPort: 0,
      receiverError: 'port unavailable',
      traceCount: 0,
      totalSpans: 0,
      spansPerMin: 0,
      lastReceivedUnixMs: 0,
      spansDropped: 0,
    }
    expect(tracingItem?.hidden?.()).toBe(false)
  })
})
