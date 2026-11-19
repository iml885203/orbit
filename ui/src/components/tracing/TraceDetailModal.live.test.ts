import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte'
import { tick } from 'svelte'
import type { Trace } from '$lib/types.gen'

const mocks = vi.hoisted(() => ({
  fetchTrace: vi.fn(),
  fetchTraceLogs: vi.fn(),
  handlers: new Set<(data: unknown) => void>(),
}))
vi.mock('$lib/tracing.svelte', () => ({ fetchTrace: mocks.fetchTrace, fetchTraceLogs: mocks.fetchTraceLogs }))
vi.mock('$lib/eventbus', () => ({ subscribe: (_: string, handler: (data: unknown) => void) => {
  mocks.handlers.add(handler)
  return () => { mocks.handlers.delete(handler) }
} }))
import TraceDetailModal from './TraceDetailModal.svelte'

function trace(id: string, names = ['initial span']): Trace {
  return { traceId: id, rootService: 'api', rootName: id, startUnixNano: 0, durationMs: 12,
    spanCount: names.length, services: ['api'], status: 'ok',
    spans: names.map((name, n) => ({ traceId: id, spanId: `${id}-${n}`, service: 'api', name,
      startUnixNano: n, endUnixNano: 12_000_000, durationMs: 12, status: 'ok' })) }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}
function refresh(id: string) { for (const handler of mocks.handlers) handler({ traceId: id }) }
const result = (value: Trace) => ({ trace: value, gone: false })
beforeEach(() => {
  mocks.fetchTrace.mockReset().mockImplementation(async (id: string) => result(trace(id)))
  mocks.fetchTraceLogs.mockReset().mockResolvedValue([])
})
afterEach(() => { cleanup(); mocks.handlers.clear() })

describe('live trace request ownership', () => {
  it('coalesces a burst into one active request and one latest refresh', async () => {
    const first = deferred<ReturnType<typeof result>>()
    const latest = deferred<ReturnType<typeof result>>()
    mocks.fetchTrace.mockReturnValueOnce(first.promise).mockReturnValueOnce(latest.promise)
    render(TraceDetailModal, { props: { traceId: 'one', onClose: vi.fn() } })
    await waitFor(() => expect(mocks.fetchTrace).toHaveBeenCalledTimes(1))
    for (let n = 0; n < 100; n++) refresh('one')
    expect(mocks.fetchTrace).toHaveBeenCalledTimes(1)
    first.resolve(result(trace('one')))
    await waitFor(() => expect(mocks.fetchTrace).toHaveBeenCalledTimes(2))
    latest.resolve(result(trace('one', ['initial span', 'latest span'])))
    await screen.findByRole('button', { name: /api latest span/ })
    expect(within(screen.getByRole('group', { name: 'Span timeline' })).getAllByRole('button')).toHaveLength(2)
    expect(mocks.fetchTraceLogs).toHaveBeenCalledTimes(1)
  })

  it('rejects an older trace response after switching views', async () => {
    const old = deferred<ReturnType<typeof result>>()
    mocks.fetchTrace.mockReturnValueOnce(old.promise)
    const view = render(TraceDetailModal, { props: { traceId: 'old', onClose: vi.fn() } })
    await waitFor(() => expect(mocks.fetchTrace).toHaveBeenCalledTimes(1))
    const signal = mocks.fetchTrace.mock.calls[0][1] as AbortSignal
    await view.rerender({ traceId: 'new', onClose: vi.fn() })
    await waitFor(() => expect(screen.getByRole('dialog')).toHaveAccessibleName('api new'))
    old.resolve(result(trace('old'))); await tick()
    expect(screen.getByRole('dialog')).toHaveAccessibleName('api new')
    expect(signal?.aborted).toBe(true)
  })

  it('keeps old initial log responses out of the new trace', async () => {
    const oldLogs = deferred<string[]>()
    mocks.fetchTraceLogs.mockReturnValueOnce(oldLogs.promise).mockResolvedValue(['new trace log'])
    const view = render(TraceDetailModal, { props: { traceId: 'old', onClose: vi.fn() } })
    await waitFor(() => expect(mocks.fetchTraceLogs).toHaveBeenCalledTimes(1))
    await view.rerender({ traceId: 'new', onClose: vi.fn() })
    await screen.findByText('new trace log')
    oldLogs.resolve(['old trace log']); await tick()
    expect(screen.queryByText('old trace log')).not.toBeInTheDocument()
    expect(screen.getByText('new trace log')).toBeInTheDocument()
  })

  it('does not open old log modal data after a trace switch', async () => {
    const oldLogs = deferred<string[]>()
    const view = render(TraceDetailModal, { props: { traceId: 'old', onClose: vi.fn() } })
    await waitFor(() => expect(mocks.fetchTraceLogs).toHaveBeenCalledTimes(1))
    mocks.fetchTraceLogs.mockReturnValueOnce(oldLogs.promise)
    await fireEvent.click(screen.getByRole('button', { name: 'Open all logs ↗' }))
    await view.rerender({ traceId: 'new', onClose: vi.fn() })
    await waitFor(() => expect(screen.getByRole('dialog')).toHaveAccessibleName('api new'))
    oldLogs.resolve(['old modal log']); await tick()
    expect(screen.getAllByRole('dialog')).toHaveLength(1)
    expect(screen.queryByText('old modal log')).not.toBeInTheDocument()
  })

  it('aborts and discards pending work on unmount', async () => {
    const pending = deferred<ReturnType<typeof result>>()
    mocks.fetchTrace.mockReturnValueOnce(pending.promise)
    const view = render(TraceDetailModal, { props: { traceId: 'one', onClose: vi.fn() } })
    await waitFor(() => expect(mocks.fetchTrace).toHaveBeenCalledTimes(1))
    const signal = mocks.fetchTrace.mock.calls[0][1] as AbortSignal
    view.unmount()
    pending.resolve(result(trace('one'))); await tick()
    expect(signal?.aborted).toBe(true)
    expect(mocks.fetchTraceLogs).not.toHaveBeenCalled()
    expect(mocks.handlers.size).toBe(0)
  })
})
