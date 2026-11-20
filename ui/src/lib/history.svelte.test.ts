import { afterEach, describe, expect, it, vi } from 'vitest'
import { history, startHistoryStream } from './history.svelte'

afterEach(() => {
  history.records = []
  vi.unstubAllGlobals()
})

describe('history stream', () => {
  it('adds and updates records through the shared bus, then stops on cleanup', () => {
    const listeners = new Map<string, (event: MessageEvent) => void>()
    vi.stubGlobal('EventSource', class {
      addEventListener(type: string, callback: (event: MessageEvent) => void) { listeners.set(type, callback) }
      close() {}
    })
    const emit = (record: unknown) => listeners.get('history')?.({ data: JSON.stringify(record) } as MessageEvent)
    const cleanup = startHistoryStream()
    try {
      const pending = { id: 'one', timestamp: '2026-11-20T00:00:00Z', source: 'cli', command: 'orbit up', hasCLI: true, status: 'pending' }
      emit(pending)
      expect(history.latest).toEqual(pending)
      emit({ ...pending, status: 'ok', durationMs: 20 })
      expect(history.records).toHaveLength(1)
      expect(history.latest).toMatchObject({ id: 'one', status: 'ok', durationMs: 20 })
      emit({ ...pending, id: 'two' })
      expect(history.records.map((record) => record.id)).toEqual(['two', 'one'])
      cleanup()
      emit({ ...pending, id: 'after-cleanup' })
      expect(history.records.map((record) => record.id)).toEqual(['two', 'one'])
    } finally { cleanup() }
  })
})
