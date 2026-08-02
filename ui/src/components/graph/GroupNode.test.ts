import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte'
import GroupNode from './GroupNode.svelte'
import { store } from '../../lib/stores.svelte'
import type { GraphResponse } from '../../lib/types.gen'

function groupGraph(state: string): GraphResponse {
  return {
    env: 'local',
    groups: [{ name: 'app', services: ['api'] }],
    nodes: [{ name: 'api', kind: 'backend', state }],
    edges: [],
  }
}

describe('GroupNode', () => {
  afterEach(() => {
    cleanup()
    store.graph.data = null
    store.graph.preview = null
    store.daemon.envs = null
    vi.restoreAllMocks()
  })

  it('offers only the useful lifecycle action and stops the group as one request', async () => {
    store.graph.data = groupGraph('healthy')
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200 }),
    )
    const running = render(GroupNode, {
      props: { data: { name: 'app', serviceCount: 1 } },
    })

    expect(running.getByRole('button', { name: 'Start app group' })).toBeDisabled()
    const stop = running.getByRole('button', { name: 'Stop app group' })
    expect(stop).toBeEnabled()
    await fireEvent.click(stop)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/down', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ groups: ['app'] }),
    }))

    running.unmount()
    store.graph.data = groupGraph('stopped')
    const stopped = render(GroupNode, {
      props: { data: { name: 'app', serviceCount: 1 } },
    })
    expect(stopped.getByRole('button', { name: 'Start app group' })).toBeEnabled()
    expect(stopped.getByRole('button', { name: 'Stop app group' })).toBeDisabled()
  })

  it('locks both lifecycle directions while the group is changing', () => {
    store.graph.data = {
      env: 'local',
      groups: [{ name: 'app', services: ['api', 'worker'] }],
      nodes: [
        { name: 'api', kind: 'backend', state: 'starting' },
        { name: 'worker', kind: 'backend', state: 'pending' },
      ],
      edges: [],
    }
    const view = render(GroupNode, {
      props: { data: { name: 'app', serviceCount: 2 } },
    })

    const start = view.getByRole('button', { name: 'Start app group' })
    const stop = view.getByRole('button', { name: 'Stop app group' })
    expect(start).toBeDisabled()
    expect(stop).toBeDisabled()
    expect(start).toHaveAttribute('aria-busy', 'true')
    expect(stop).toHaveAttribute('aria-busy', 'true')
  })
})
