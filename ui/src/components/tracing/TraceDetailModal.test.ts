import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'

const TRACE = {
  traceId: 'abc123def4567890',
  rootService: 'api',
  rootName: 'GET /x',
  startUnixNano: 0,
  durationMs: 12,
  spanCount: 1,
  services: ['api'],
  status: 'ok',
  spans: [
    {
      traceId: 'abc123def4567890',
      spanId: 's1',
      service: 'api',
      name: 'GET /x',
      startUnixNano: 0,
      endUnixNano: 12_000_000,
      durationMs: 12,
      status: 'ok',
    },
  ],
}

vi.mock('$lib/tracing.svelte', () => ({
  fetchTrace: vi.fn(async () => ({ trace: TRACE, gone: false })),
  fetchTraceLogs: vi.fn(async () => []),
}))
// subscribe returns an unsubscribe fn; the modal calls it in an $effect cleanup.
vi.mock('$lib/eventbus', () => ({ subscribe: () => () => {} }))

import TraceDetailModal from './TraceDetailModal.svelte'

afterEach(cleanup)

describe('TraceDetailModal', () => {
  it('renders a labelled modal dialog for the loaded trace', async () => {
    render(TraceDetailModal, { props: { traceId: TRACE.traceId, onClose: vi.fn() } })

    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    // aria-labelledby points at the trace heading, which fills in after load.
    await waitFor(() => expect(dialog).toHaveAccessibleName(/api GET \/x/))
  })

  it('closes on Escape', async () => {
    const onClose = vi.fn()
    render(TraceDetailModal, { props: { traceId: TRACE.traceId, onClose } })
    await screen.findByRole('dialog')

    await fireEvent.keyDown(window, { key: 'Escape' })

    expect(onClose).toHaveBeenCalledOnce()
  })

  it('closes via the labelled close button', async () => {
    const onClose = vi.fn()
    render(TraceDetailModal, { props: { traceId: TRACE.traceId, onClose } })

    await fireEvent.click(await screen.findByRole('button', { name: 'Close trace detail' }))

    expect(onClose).toHaveBeenCalledOnce()
  })
})
