import { fireEvent, render, screen } from '@testing-library/svelte'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TracingPage from './TracingPage.svelte'
import { store } from '$lib/stores.svelte'
import { tracing } from '$lib/tracing.svelte'

const { push, replace, fetchTraces, fetchTracingStatus, subscribeLiveTraces } = vi.hoisted(() => ({
  push: vi.fn(),
  replace: vi.fn(),
  fetchTraces: vi.fn(),
  fetchTracingStatus: vi.fn(),
  subscribeLiveTraces: vi.fn(() => vi.fn()),
}))

vi.mock('svelte-spa-router', () => ({
  push,
  replace,
  router: { querystring: '' },
}))
vi.mock('$lib/tracing.svelte', async (importOriginal) => {
  const actual = await importOriginal<typeof import('$lib/tracing.svelte')>()
  return { ...actual, fetchTraces, fetchTracingStatus, subscribeLiveTraces }
})
vi.mock('../components/tracing/TraceTable.svelte', () => ({ default: () => null }))
vi.mock('../components/tracing/TraceDetailModal.svelte', () => ({ default: () => null }))

describe('TracingPage empty state', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    tracing.reset()
    tracing.status = null
    store.daemon.services = {}
    fetchTraces.mockResolvedValue([])
    fetchTracingStatus.mockResolvedValue({
      configured: true,
      receiverHealthy: true,
      otlpPort: 4318,
      actualPort: 4318,
      lastReceivedUnixMs: 0,
      spansPerMin: 0,
    })
  })

  it('routes users to Services instead of teaching a CLI prerequisite', async () => {
    render(TracingPage)
    const open = await screen.findByRole('button', { name: 'Open Services' })

    await fireEvent.click(open)

    expect(push).toHaveBeenCalledWith('/')
    expect(screen.queryByText('orbit up')).not.toBeInTheDocument()
  })
})
