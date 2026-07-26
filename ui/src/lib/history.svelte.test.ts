import { afterEach, describe, expect, it, vi } from 'vitest'
import { startHistoryStream } from './history.svelte'

describe('startHistoryStream', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('opens one EventSource when started', () => {
    const close = vi.fn()
    const events: Array<{ url: string; close: ReturnType<typeof vi.fn> }> = []

    vi.stubGlobal('EventSource', class {
      onopen: (() => void) | null = null
      onerror: (() => void) | null = null

      constructor(url: string) {
        events.push({ url, close })
      }

      addEventListener() {}
      close = close
    })

    const cleanup = startHistoryStream()

    // startHistoryStream now joins the shared /api/events bus; the first
    // subscriber opens it once and the last unsubscriber closes it.
    expect(events.map((e) => e.url)).toEqual(['/api/events'])
    expect(close).not.toHaveBeenCalled()

    cleanup()
    expect(close).toHaveBeenCalledTimes(1)
  })
})
