import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
const mocks = vi.hoisted(() => ({ toast: vi.fn() }))
vi.mock('./stores.svelte', () => ({ toast: mocks.toast }))
import { fetchTrace, fetchTraceLogs } from './tracing.svelte'

const fetchMock = vi.fn()
beforeEach(() => {
  mocks.toast.mockReset()
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('trace request cancellation', () => {
  it('passes the owning signal and keeps intentional cancellation silent', async () => {
    const controller = new AbortController()
    controller.abort()
    fetchMock.mockRejectedValue(new DOMException('canceled', 'AbortError'))
    expect(await fetchTrace('old', controller.signal)).toEqual({ trace: null, gone: false })
    expect(await fetchTraceLogs('old', controller.signal)).toEqual([])
    expect(fetchMock).toHaveBeenCalledWith('/api/traces/old', { signal: controller.signal })
    expect(fetchMock).toHaveBeenCalledWith('/api/traces/old/logs', { signal: controller.signal })
    expect(mocks.toast).not.toHaveBeenCalled()
  })

  it('still reports genuine request failures', async () => {
    fetchMock.mockRejectedValue(new Error('offline'))
    await fetchTrace('current')
    await fetchTraceLogs('current')
    expect(mocks.toast).toHaveBeenCalledWith('trace unavailable')
    expect(mocks.toast).toHaveBeenCalledWith('trace logs unavailable')
  })

  it('keeps expired traces distinct from transport errors', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 404 })
    expect(await fetchTrace('expired')).toEqual({ trace: null, gone: true })
    expect(mocks.toast).not.toHaveBeenCalled()
  })
})
