import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

class FakeEventSource {
  static instances: FakeEventSource[] = []
  closed = false
  onerror: (() => void) | null = null
  listeners = new Map<string, Array<(event: MessageEvent) => void>>()
  constructor(readonly url: string) { FakeEventSource.instances.push(this) }
  addEventListener(type: string, callback: (event: MessageEvent) => void) {
    const callbacks = this.listeners.get(type) ?? []
    callbacks.push(callback)
    this.listeners.set(type, callbacks)
  }
  close() { this.closed = true }
  emit(type: string, data: unknown) {
    for (const callback of this.listeners.get(type) ?? []) callback({ data: JSON.stringify(data) } as MessageEvent)
  }
}

const active = () => FakeEventSource.instances.filter((source) => !source.closed)

beforeEach(() => {
  vi.resetModules()
  vi.useFakeTimers()
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })

describe('shared event connection ownership', () => {
  it('keeps one connection when a subscriber joins during reconnect', async () => {
    const { subscribe } = await import('./eventbus')
    const first = subscribe('status', vi.fn())
    FakeEventSource.instances[0].onerror?.()
    expect(active()).toHaveLength(0)
    const second = subscribe('trace', vi.fn())
    expect(active()).toHaveLength(1)
    vi.advanceTimersByTime(3000)
    expect(active()).toHaveLength(1)
    first(); second()
    expect(active()).toHaveLength(0)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('ignores callbacks and frames from an obsolete connection', async () => {
    const { subscribe } = await import('./eventbus')
    const onStatus = vi.fn()
    const unsub = subscribe('status', onStatus)
    const old = FakeEventSource.instances[0]
    old.onerror?.()
    vi.advanceTimersByTime(3000)
    const current = active()[0]
    old.onerror?.()
    old.emit('status', { old: true })
    expect(active()).toEqual([current])
    current.emit('status', { current: true })
    expect(onStatus).toHaveBeenCalledExactlyOnceWith({ current: true })
    unsub()
    expect(active()).toHaveLength(0)
  })

  it('cancels reconnection on final unsubscribe and permits a clean resubscribe', async () => {
    const { subscribe } = await import('./eventbus')
    const unsub = subscribe('status', vi.fn())
    FakeEventSource.instances[0].onerror?.()
    unsub(); unsub()
    vi.advanceTimersByTime(6000)
    expect(active()).toHaveLength(0)
    const handler = vi.fn()
    const next = subscribe('status', handler)
    active()[0].emit('status', { ready: true })
    expect(handler).toHaveBeenCalledExactlyOnceWith({ ready: true })
    next()
    expect(active()).toHaveLength(0)
  })

  it('shares one listener per type and notifies disconnect subscribers', async () => {
    const { subscribe, onConnectionLost } = await import('./eventbus')
    const lost = vi.fn(); const stopLost = onConnectionLost(lost)
    const first = vi.fn(); const second = vi.fn()
    const stopFirst = subscribe('log', first); const stopSecond = subscribe('log', second)
    expect(active()[0].listeners.get('log')).toHaveLength(1)
    active()[0].emit('log', { line: 'one' })
    expect(first).toHaveBeenCalledOnce(); expect(second).toHaveBeenCalledOnce()
    active()[0].onerror?.()
    expect(lost).toHaveBeenCalledOnce()
    stopLost(); stopFirst(); stopSecond()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('retains a repeated handler until each subscription is released', async () => {
    const { subscribe } = await import('./eventbus')
    const handler = vi.fn()
    const first = subscribe('status', handler); const second = subscribe('status', handler)
    first(); first()
    active()[0].emit('status', { ready: true })
    expect(handler).toHaveBeenCalledOnce()
    second()
    expect(active()).toHaveLength(0)
  })
})
