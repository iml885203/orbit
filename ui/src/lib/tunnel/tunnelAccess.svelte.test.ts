import { afterEach, describe, expect, it, vi } from 'vitest'
import { startTunnelAccessStream, tunnelAccess } from './tunnelAccess.svelte'
import type { AccessLine } from './api'

// Drives the shared eventbus: captures the 'tunnel-access' listener the bus
// attaches, so a test can dispatch events (and simulate the server replaying
// its retained ring on reconnect).
function stubBus() {
  const listeners: Record<string, (e: { data: string }) => void> = {}
  vi.stubGlobal('EventSource', class {
    onopen: (() => void) | null = null
    onerror: (() => void) | null = null
    constructor() {}
    addEventListener(type: string, fn: (e: { data: string }) => void) {
      listeners[type] = fn
    }
    close() {}
  })
  return {
    emit(l: AccessLine) {
      listeners['tunnel-access']?.({ data: JSON.stringify(l) })
    },
  }
}

const line = (over: Partial<AccessLine> = {}): AccessLine => ({
  local_port: 8080,
  time: '2026-06-05T10:00:00Z',
  method: 'POST',
  path: '/callbacks/provider-a/getbalance',
  status: 200,
  duration_ms: 4,
  ...over,
})

describe('startTunnelAccessStream', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    for (const k of Object.keys(tunnelAccess.byPort)) delete tunnelAccess.byPort[Number(k)]
  })

  it('dedupes replayed lines on reconnect', () => {
    const bus = stubBus()
    const cleanup = startTunnelAccessStream()

    bus.emit(line())
    bus.emit(line()) // identical line replayed (e.g. SSE reconnect)

    expect(tunnelAccess.byPort[8080]).toHaveLength(1)
    cleanup()
  })

  it('keeps distinct lines', () => {
    const bus = stubBus()
    const cleanup = startTunnelAccessStream()

    bus.emit(line({ duration_ms: 4 }))
    bus.emit(line({ duration_ms: 9 }))
    bus.emit(line({ path: '/callbacks/provider-a/rollback' }))

    expect(tunnelAccess.byPort[8080]).toHaveLength(3)
    cleanup()
  })
})
